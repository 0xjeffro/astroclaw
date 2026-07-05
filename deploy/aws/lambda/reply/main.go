package main

import (
	"astroclaw/pkg/agent"
	"astroclaw/pkg/api"
	"astroclaw/pkg/app/agents"
	"astroclaw/pkg/app/chat"
	"astroclaw/pkg/app/notes"
	"astroclaw/pkg/app/passwords"
	"astroclaw/pkg/app/settings"
	appskills "astroclaw/pkg/app/skills"
	"astroclaw/pkg/app/system"
	"astroclaw/pkg/cloud"
	"astroclaw/pkg/cloud/wsbus"
	wsaws "astroclaw/pkg/cloud/wsbus/aws"
	"astroclaw/pkg/provider"
	"astroclaw/pkg/tool"
	toolskills "astroclaw/pkg/tool/skills"
	"astroclaw/pkg/tool/webfetch"
	"astroclaw/pkg/tool/websearch"
	"context"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/apigatewaymanagementapi"
	"github.com/awslabs/aurora-dsql-connectors/go/pgx/dsql"
	"github.com/awslabs/aws-lambda-go-api-proxy/httpadapter"
	"github.com/go-chi/chi/v5"
)

// buildLambdaHandler wires DSQL, wsbus, storage/kms, chat.Service, and
// the api.ReplyHandler, then mounts POST /sessions/{sessionID}/reply on
// a chi router and returns a Lambda-compatible adapter.
func buildLambdaHandler() *httpadapter.HandlerAdapterV2 {
	ctx := context.Background()

	pool, err := dsql.NewPool(ctx, dsql.Config{
		Host: os.Getenv("DSQL_ENDPOINT"),
	})
	if err != nil {
		log.Fatalf("connect to DSQL: %v", err)
	}

	systemSvc := system.NewService(pool)
	wsRegistry := wsbus.NewWsRegistry(systemSvc)

	// AWS-specific WebSocket bus: API Gateway holds the socket, we push
	// via PostToConnection. WS_ENDPOINT is the @connections endpoint of
	// the deployed WS stage.
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("load AWS config: %v", err)
	}
	apigwClient := apigatewaymanagementapi.NewFromConfig(awsCfg, func(o *apigatewaymanagementapi.Options) {
		o.BaseEndpoint = new(os.Getenv("WS_ENDPOINT"))
	})
	bus := wsaws.NewBus(apigwClient)

	// Credentials.
	km, err := cloud.OpenKeyManager(ctx, os.Getenv("KMS_URL"))
	if err != nil {
		log.Fatalf("open key manager: %v", err)
	}
	pwSvc := passwords.NewService(pool, km)

	llmCred, err := pwSvc.GetSystemCredential(ctx, passwords.SystemCredAnthropicAPIKey)
	if err != nil {
		log.Fatalf("read LLM API key: %v (deploy with --parameters AnthropicApiKey=sk-ant-xxx)", err)
	}

	agentsSvc := agents.NewService(pool)
	settingsSvc := settings.NewService(pool)
	notesSvc := notes.NewService(pool)
	skillsSvc := appskills.NewService(pool)

	bucket, err := cloud.OpenBucket(ctx, os.Getenv("STORAGE_URL"))
	if err != nil {
		log.Fatalf("open storage bucket: %v", err)
	}

	// ReplyHandler is built before chat.Service because the streaming
	// callbacks (OnTextDelta / OnToolCall / OnToolResult) call its
	// PushToSession. Chat is plugged in after chat.NewService returns.
	replyH := &api.ReplyHandler{
		Bus:      bus,
		Registry: wsRegistry,
	}

	createFn := func(s *chat.Session, agentID string) (*agent.Agent, error) {
		agentProfile, err := agentsSvc.GetAgentFromWorkspace(context.Background(), s.WorkspaceID, agentID)
		if err != nil {
			return nil, fmt.Errorf("agent %q not found: %w", agentID, err)
		}

		p := provider.NewAnthropic(llmCred.Value, agentProfile.Model)
		systemPrompt := buildPrompt(context.Background(), s.WorkspaceID, agentProfile, s.UserID, settingsSvc, notesSvc, skillsSvc)

		toolRegistry := tool.NewRegistry()
		toolRegistry.Register(&tool.TimeTool{})
		toolRegistry.Register(&tool.ArithmeticTool{})
		toolRegistry.Register(&tool.ReadFileTool{})
		// TODO: remove ExecCommand. Prompt-injected agent could reach DSQL creds.
		toolRegistry.Register(&tool.ExecCommandTool{})
		toolRegistry.Register(&tool.WriteFileTool{})
		toolRegistry.Register(&tool.EditFileTool{})
		toolRegistry.Register(&tool.MemorySaveTool{
			Store:     &notes.MemoryStoreAdapter{Service: notesSvc},
			AgentID:   agentID,
			UserID:    s.UserID,
			SessionID: s.ID,
		})
		toolRegistry.Register(&websearch.Tool{
			Provider: websearch.NewDuckDuckGoProvider(),
		})
		toolRegistry.Register(webfetch.New())
		toolRegistry.Register(&toolskills.Tool{
			Skills:      skillsSvc,
			Bucket:      bucket,
			WorkspaceID: s.WorkspaceID,
		})

		a := agent.NewFromContext(
			p, systemPrompt, toolRegistry, 128000,
			chat.ToProviderMessages(s.ContextMessages), s.ContextSummary,
		)

		// Snapshot pushed to all WS clients on every agent state update.
		state := &chat.WSEvent{
			SessionID: s.ID,
			AgentID:   agentID,
			Status:    chat.WSStatusStreaming,
		}

		a.OnTextDelta = func(text string) {
			state.Text += text
			state.Status = chat.WSStatusStreaming
			replyH.PushToSession(context.Background(), s.ID, *state)
		}

		a.OnToolCall = func(id, toolName, args string) {
			state.ToolCalls = append(state.ToolCalls, chat.WSToolCall{
				ID:        id,
				Name:      toolName,
				Arguments: args,
				Status:    chat.WSToolStatusRunning,
			})
			state.Status = chat.WSStatusToolCalling
			replyH.PushToSession(context.Background(), s.ID, *state)
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
			replyH.PushToSession(context.Background(), s.ID, *state)
		}

		return a, nil
	}

	svc := chat.NewService(pool, createFn)
	replyH.Chat = svc

	r := chi.NewRouter()
	r.Post("/sessions/{sessionID}/reply", replyH.ServeHTTP)

	return httpadapter.NewV2(r)
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

func main() {
	lambda.Start(buildLambdaHandler().ProxyWithContext)
}
