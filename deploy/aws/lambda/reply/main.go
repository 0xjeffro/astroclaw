package main

import (
	"astroclaw/pkg/agent"
	"astroclaw/pkg/app/agents"
	"astroclaw/pkg/app/chat"
	"astroclaw/pkg/app/notes"
	"astroclaw/pkg/app/passwords"
	"astroclaw/pkg/app/settings"
	appskills "astroclaw/pkg/app/skills"
	"astroclaw/pkg/app/system"
	"astroclaw/pkg/crypto"
	"astroclaw/pkg/provider"
	"astroclaw/pkg/tool"
	toolskills "astroclaw/pkg/tool/skills"
	"astroclaw/pkg/tool/webfetch"
	"astroclaw/pkg/tool/websearch"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/apigatewaymanagementapi"
	apigwtypes "github.com/aws/aws-sdk-go-v2/service/apigatewaymanagementapi/types"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/awslabs/aurora-dsql-connectors/go/pgx/dsql"
)

var (
	svc         *chat.Service
	systemSvc   *system.Service
	apigwClient *apigatewaymanagementapi.Client
)

// Init runs once when Lambda cold-starts. Connects to DSQL, sets up
// credentials and chat service. System prompt is built per-session
// in createFn so it always reads the latest settings and memories.
func init() {
	ctx := context.Background()

	pool, err := dsql.NewPool(ctx, dsql.Config{
		Host: os.Getenv("DSQL_ENDPOINT"),
	})
	if err != nil {
		log.Fatalf("connect to DSQL: %v", err)
	}

	systemSvc = system.NewService(pool)

	// API Gateway Management API client for pushing WebSocket events.
	// https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-how-to-call-websocket-api-connections.html
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("load AWS config: %v", err)
	}
	apigwClient = apigatewaymanagementapi.NewFromConfig(awsCfg, func(o *apigatewaymanagementapi.Options) {
		o.BaseEndpoint = new(os.Getenv("WS_ENDPOINT"))
	})

	// Read credentials.
	km := crypto.NewAWSKMSKeyManager(kms.NewFromConfig(awsCfg), os.Getenv("PASSWORDS_KMS_KEY_ID"))
	pwSvc := passwords.NewService(pool, km)

	// TODO: Here we should design a more flexible way to obtain the LLM API key.
	// For exp, if anti's is not set, fall back to openai. This should be accompanied by some config files to set how the fallback should be handled.
	// Another point is, it the user has configured their own key, it should take priority over the user's own.
	// Furthermore, if the user's key is down, we should fall back to the system's key instead of just erroring out.
	llmCred, err := pwSvc.GetSystemCredential(ctx, "anthropic-api-key")
	if err != nil {
		log.Fatalf("read LLM API key: %v (deploy with --parameters AnthropicApiKey=sk-ant-xxx)", err)
	}
	//
	//apiKeyCred, err := pwSvc.GetCredentialByName(ctx, "api-key")
	//if err != nil {
	//	log.Println("warning: no api-key in credentials table, all requests will be allowed without authentication")
	//} else {
	//	apiKey = apiKeyCred.Value
	//}

	var p provider.Provider
	model := os.Getenv("MODEL_NAME")
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}
	p = provider.NewAnthropic(llmCred.Value, model)

	agentsSvc := agents.NewService(pool)
	settingsSvc := settings.NewService(pool)
	notesSvc := notes.NewService(pool)
	skillsSvc := appskills.NewService(pool)

	// S3 client for skill file storage.
	s3Client := s3.NewFromConfig(awsCfg)
	skillsBucket := os.Getenv("SKILLS_BUCKET")

	createFn := func(s *chat.Session, agentID string) (*agent.Agent, error) {
		// Load agent profile dynamically per request.
		agentProfile, err := agentsSvc.GetAgentFromWorkspace(context.Background(), s.WorkspaceID, agentID)
		if err != nil {
			return nil, fmt.Errorf("agent %q not found: %w", agentID, err)
		}

		systemPrompt := buildPrompt(context.Background(), s.WorkspaceID, agentProfile, s.UserID, settingsSvc, notesSvc, skillsSvc)

		registry := tool.NewRegistry()
		registry.Register(&tool.TimeTool{})
		registry.Register(&tool.ArithmeticTool{})
		registry.Register(&tool.ReadFileTool{})
		// TODO: remove ExecCommand tool. In Lambda, a prompt-injected Agent could
		// use ExecCommand to access DSQL credentials and compromise the database.
		registry.Register(&tool.ExecCommandTool{})
		registry.Register(&tool.WriteFileTool{})
		registry.Register(&tool.EditFileTool{})
		registry.Register(&tool.MemorySaveTool{
			Store:     &notes.MemoryStoreAdapter{Service: notesSvc},
			AgentID:   agentID,
			UserID:    s.UserID,
			SessionID: s.ID,
		})
		registry.Register(&websearch.Tool{
			Provider: websearch.NewDuckDuckGoProvider(),
		})
		registry.Register(webfetch.New())
		registry.Register(&toolskills.Tool{
			Skills:      skillsSvc,
			S3Client:    s3Client,
			Bucket:      skillsBucket,
			WorkspaceID: s.WorkspaceID,
		})

		a := agent.NewFromContext(
			p, systemPrompt, registry, 128000,
			chat.ToProviderMessages(s.ContextMessages), s.ContextSummary,
		)

		// Maintain a state snapshot that gets pushed to all WebSocket clients
		// on every update. Each push is a full snapshot, not a delta.
		state := &chat.WSEvent{
			SessionID: s.ID,
			AgentID:   agentID,
			Status:    chat.WSStatusStreaming,
		}

		a.OnTextDelta = func(text string) {
			state.Text += text
			state.Status = chat.WSStatusStreaming
			pushToSession(context.Background(), s.ID, *state)
		}

		a.OnToolCall = func(id, toolName, args string) {
			state.ToolCalls = append(state.ToolCalls, chat.WSToolCall{
				ID:        id,
				Name:      toolName,
				Arguments: args,
				Status:    chat.WSToolStatusRunning,
			})
			state.Status = chat.WSStatusToolCalling
			pushToSession(context.Background(), s.ID, *state)
		}

		a.OnToolResult = func(id, toolName, result string) {
			for i := range state.ToolCalls {
				if state.ToolCalls[i].ID == id {
					state.ToolCalls[i].Status = chat.WSToolStatusCompleted
					state.ToolCalls[i].Result = result
					break
				}
			}
			state.Status = chat.WSStatusStreaming
			pushToSession(context.Background(), s.ID, *state)
		}

		return a, nil
	}

	svc = chat.NewService(pool, createFn)
}

func buildPrompt(ctx context.Context, workspaceID string, agentProfile *agents.Agent, userID string, settingsSvc *settings.Service, notesSvc *notes.Service, skillsSvc *appskills.Service) string {
	var cfg agent.PromptConfig
	cfg.Soul = agentProfile.Soul
	if user, err := settingsSvc.GetUserSetting(ctx, userID, settings.SettingUserProfile); err == nil {
		cfg.User = user.Value
	}
	if memories, err := notesSvc.FormatUserMemoryForPrompt(ctx, agentProfile.ID, userID, agent.DefaultCharLimits().Memories); err == nil {
		cfg.Memories = memories
	}
	if skillList, err := skillsSvc.ListSkillsByWorkspace(ctx, workspaceID); err == nil {
		cfg.Skills = appskills.FormatForPrompt(skillList)
	}
	return agent.BuildSystemPrompt(cfg, agent.DefaultCharLimits())
}

func handler(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	//if apiKey != "" && req.Headers["x-api-key"] != apiKey {
	//	return jsonResponse(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	//}

	// Extract session ID from path: /sessions/{id}/reply
	path := req.RequestContext.HTTP.Path
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 3 || parts[0] != "sessions" || parts[2] != "reply" || req.RequestContext.HTTP.Method != "POST" {
		return jsonResponse(http.StatusNotFound, map[string]string{"error": "not found"})
	}
	sessionID := parts[1]
	// TODO: authz — load session, verify caller has access to session.WorkspaceID.

	var body struct {
		Text    string `json:"text"`
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
		return jsonResponse(http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
	}

	reply, err := svc.Reply(ctx, sessionID, body.AgentID, body.Text)
	if err != nil {
		pushToSession(ctx, sessionID, chat.WSEvent{
			SessionID: sessionID,
			AgentID:   body.AgentID,
			Status:    chat.WSStatusError,
			Error:     err.Error(),
		})
		return jsonResponse(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	pushToSession(ctx, sessionID, chat.WSEvent{
		SessionID: sessionID,
		AgentID:   body.AgentID,
		Status:    chat.WSStatusDone,
		Text:      reply,
	})
	return jsonResponse(http.StatusOK, map[string]string{"reply": reply})
}

func jsonResponse(status int, body any) (events.APIGatewayV2HTTPResponse, error) {
	b, _ := json.Marshal(body)
	return events.APIGatewayV2HTTPResponse{
		StatusCode: status,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(b),
	}, nil
}

// pushToSession pushes a WebSocket event to all connected members of a session.
// It queries session members, looks up each member's active connections, and
// calls PostToConnection for each one. Dead connections (410 Gone) are cleaned up.
func pushToSession(ctx context.Context, sessionID string, event chat.WSEvent) {
	members, err := svc.ListSessionMembers(ctx, sessionID)
	if err != nil {
		log.Printf("pushToSession: list members for session %s: %v", sessionID, err)
		return
	}

	payload, err := json.Marshal(event)
	if err != nil {
		log.Printf("pushToSession: marshal event: %v", err)
		return
	}

	for _, m := range members {
		conns, err := systemSvc.GetConnectionsByUser(ctx, m.UserID)
		if err != nil {
			log.Printf("pushToSession: get connections for user %s: %v", m.UserID, err)
			continue
		}
		for _, c := range conns {
			_, err := apigwClient.PostToConnection(ctx, &apigatewaymanagementapi.PostToConnectionInput{
				ConnectionId: &c.ConnectionID,
				Data:         payload,
			})
			if err != nil {
				// 410 Gone means the client disconnected but $disconnect didn't fire.
				// https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-how-to-call-websocket-api-connections.html
				// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/apigatewaymanagementapi/types#GoneException
				if _, ok := errors.AsType[*apigwtypes.GoneException](err); ok {
					log.Printf("pushToSession: connection %s is gone, cleaning up", c.ConnectionID)
					_ = systemSvc.DeleteWSConnectRecord(ctx, c.ConnectionID)
					continue
				}
				log.Printf("pushToSession: post to connection %s: %v", c.ConnectionID, err)
			}
		}
	}
}

func main() {
	lambda.Start(handler)
}
