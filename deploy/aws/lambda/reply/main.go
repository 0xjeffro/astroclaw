package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iclaw/pkg/agent"
	"iclaw/pkg/app/agents"
	"iclaw/pkg/app/chat"
	"iclaw/pkg/app/notes"
	"iclaw/pkg/app/passwords"
	"iclaw/pkg/app/settings"
	"iclaw/pkg/app/system"
	"iclaw/pkg/provider"
	"iclaw/pkg/tool"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/apigatewaymanagementapi"
	apigwtypes "github.com/aws/aws-sdk-go-v2/service/apigatewaymanagementapi/types"
	"github.com/awslabs/aurora-dsql-connectors/go/pgx/dsql"
)

var (
	svc         *chat.Service
	systemSvc   *system.Service
	apigwClient *apigatewaymanagementapi.Client
	apiKey      string
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
	pwSvc := passwords.NewService(pool)

	llmCred, err := pwSvc.GetCredentialByName(ctx, "anthropic-api-key")
	if err != nil {
		log.Fatalf("read LLM API key: %v (deploy with --parameters AnthropicApiKey=sk-ant-xxx)", err)
	}

	apiKeyCred, err := pwSvc.GetCredentialByName(ctx, "api-key")
	if err != nil {
		log.Println("warning: no api-key in credentials table, all requests will be allowed without authentication")
	} else {
		apiKey = apiKeyCred.Value
	}

	var p provider.Provider
	model := os.Getenv("MODEL_NAME")
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}
	p = provider.NewAnthropic(llmCred.Value, model)

	agentsSvc := agents.NewService(pool)
	settingsSvc := settings.NewService(pool)
	notesSvc := notes.NewService(pool)

	createFn := func(s *chat.Session, agentID string) (*agent.Agent, error) {
		// Load agent profile dynamically per request.
		agentProfile, err := agentsSvc.GetAgent(context.Background(), agentID)
		if err != nil {
			return nil, fmt.Errorf("agent %q not found: %w", agentID, err)
		}

		systemPrompt := buildPrompt(context.Background(), agentProfile, settingsSvc, notesSvc)

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
			SessionID: s.ID,
		})

		return agent.NewFromContext(
			p, systemPrompt, registry, 128000,
			chat.ToProviderMessages(s.ContextMessages), s.ContextSummary,
		), nil
	}

	svc = chat.NewService(pool, createFn)
}

func buildPrompt(ctx context.Context, agentProfile *agents.Agent, settingsSvc *settings.Service, notesSvc *notes.Service) string {
	var cfg agent.PromptConfig
	cfg.Soul = agentProfile.Soul
	if user, err := settingsSvc.GetKVSetting(ctx, settings.SettingUserProfile); err == nil {
		cfg.User = user.Value
	}
	if memories, err := notesSvc.FormatForPrompt(ctx, agentProfile.ID, agent.DefaultCharLimits().Memories); err == nil {
		cfg.Memories = memories
	}
	return agent.BuildSystemPrompt(cfg, agent.DefaultCharLimits())
}

func handler(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	if apiKey != "" && req.Headers["x-api-key"] != apiKey {
		return jsonResponse(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}

	// Extract session ID from path: /sessions/{id}/reply
	path := req.RequestContext.HTTP.Path
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 || req.RequestContext.HTTP.Method != "POST" {
		return jsonResponse(http.StatusNotFound, map[string]string{"error": "not found"})
	}
	sessionID := parts[1]

	var body struct {
		Text    string `json:"text"`
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
		return jsonResponse(http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
	}

	reply, err := svc.Reply(ctx, sessionID, body.AgentID, body.Text)
	if err != nil {
		return jsonResponse(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
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
