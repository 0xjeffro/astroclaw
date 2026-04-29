package main

import (
	"context"
	"encoding/json"
	"iclaw/pkg/agent"
	"iclaw/pkg/app/chat"
	"iclaw/pkg/app/notes"
	"iclaw/pkg/app/passwords"
	"iclaw/pkg/app/settings"
	"iclaw/pkg/provider"
	"iclaw/pkg/tool"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/awslabs/aurora-dsql-connectors/go/pgx/dsql"
)

var (
	svc    *chat.Service
	apiKey string
)

// init runs once when Lambda cold-starts. Connects to DSQL, sets up
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

	settingsSvc := settings.NewService(pool)
	notesSvc := notes.NewService(pool)

	createFn := func(s *chat.Session) *agent.Agent {
		// Build system prompt per-session so it always reads the latest
		// settings (SOUL, USER) and memories from the database.
		systemPrompt := buildPrompt(context.Background(), settingsSvc, notesSvc)

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
			SessionID: s.ID,
		})

		return agent.NewFromContext(
			p, systemPrompt, registry, 128000,
			chat.ToProviderMessages(s.ContextMessages), s.ContextSummary,
		)
	}

	svc = chat.NewService(pool, createFn, nil)
}

func buildPrompt(ctx context.Context, settingsSvc *settings.Service, notesSvc *notes.Service) string {
	var cfg agent.PromptConfig
	if soul, err := settingsSvc.GetPromptSetting(ctx, "soul"); err == nil {
		cfg.Soul = soul.Value
	}
	if user, err := settingsSvc.GetPromptSetting(ctx, "user"); err == nil {
		cfg.User = user.Value
	}
	if memories, err := notesSvc.FormatForPrompt(ctx, agent.DefaultCharLimits().Memories); err == nil {
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
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
		return jsonResponse(http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
	}

	reply, err := svc.Reply(ctx, sessionID, body.Text)
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

func main() {
	lambda.Start(handler)
}
