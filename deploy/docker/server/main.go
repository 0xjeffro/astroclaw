// astroclaw-server is the single-container entrypoint for astroclaw.
//
// It runs a long-lived HTTP server that speaks the same routes the AWS
// deployment exposes across api / reply / wsconnect / wsdisconnect
// Lambdas — collapsed into one chi router because a container process
// can hold WebSocket sockets in memory instead of paying the API
// Gateway routing tax.
//
// Deployment shape:
//
//	docker run \
//	    -e DATABASE_URL=postgres://user:pass@host/db \
//	    -e KMS_URL=base64key://<b64-master-key> \
//	    -e STORAGE_URL=file:///data/skills \
//	    -p 8080:8080 \
//	    astroclaw
//
// Single-instance assumption: this server owns every live WS socket in
// process memory. Multi-instance deploys need a PubsubBus layer above
// wsbus.Bus (out of scope for now).
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"astroclaw/pkg/agent"
	"astroclaw/pkg/api"
	"astroclaw/pkg/app/agents"
	"astroclaw/pkg/app/chat"
	"astroclaw/pkg/app/notes"
	"astroclaw/pkg/app/passwords"
	"astroclaw/pkg/app/settings"
	appskills "astroclaw/pkg/app/skills"
	"astroclaw/pkg/app/system"
	"astroclaw/pkg/auth"
	"astroclaw/pkg/cloud"
	"astroclaw/pkg/cloud/wsbus"
	"astroclaw/pkg/cloud/wsbus/inprocess"
	"astroclaw/pkg/provider"
	"astroclaw/pkg/tool"
	toolskills "astroclaw/pkg/tool/skills"
	"astroclaw/pkg/tool/webfetch"
	"astroclaw/pkg/tool/websearch"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, mustEnv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("connect to Postgres: %v", err)
	}
	defer pool.Close()

	// Cloud abstractions — URL-driven, same code path as the Lambdas.
	// Callers pick file://, mem://, s3:// via env.
	km, err := cloud.OpenKeyManager(ctx, mustEnv("KMS_URL"))
	if err != nil {
		log.Fatalf("open key manager: %v", err)
	}
	bucket, err := cloud.OpenBucket(ctx, mustEnv("STORAGE_URL"))
	if err != nil {
		log.Fatalf("open storage bucket: %v", err)
	}
	defer func() { _ = bucket.Close() }()

	// Services.
	systemSvc := system.NewService(pool)
	pwSvc := passwords.NewService(pool, km)
	settingsSvc := settings.NewService(pool)
	notesSvc := notes.NewService(pool)
	agentsSvc := agents.NewService(pool)
	skillsSvc := appskills.NewService(pool)
	secretLoader := auth.NewSecretLoader(pwSvc)

	// TODO: add graceful shutdown (signal.Notify + srv.Shutdown) so
	// WebSocket handler defers can Unregister on SIGTERM. Without it,
	// docker stop leaves ghost rows in app_system_connections. They
	// self-heal on next push via wsbus.ErrConnGone, so this is a
	// nicety, not a correctness fix.
	llmCred, err := pwSvc.GetSystemCredential(ctx, passwords.SystemCredAnthropicAPIKey)
	if err != nil {
		log.Fatalf("read LLM API key: %v", err)
	}

	// wsbus — in-process socket map + shared DSQL registry.
	wsBus := inprocess.NewBus()
	wsRegistry := wsbus.NewWsRegistry(systemSvc)

	// ReplyHandler is built before chat.Service because the createFn
	// closure below captures replyH.PushToSession. Chat is plugged in
	// after chat.NewService returns.
	replyH := &api.ReplyHandler{
		Bus:      wsBus,
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
		// TODO: remove ExecCommand. Prompt-injected agent could reach DB creds.
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

		// Snapshot pushed on every state update.
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

	chatSvc := chat.NewService(pool, createFn)
	replyH.Chat = chatSvc

	// Router: API routes + reply + WS. Same paths the CLI already speaks
	// against AWS, so no client changes are needed to point at docker.
	r := chi.NewRouter()
	r.Use(middleware.CleanPath, middleware.Recoverer)

	r.Mount("/", api.NewRouter(api.RouterConfig{
		Chat:      chatSvc,
		Settings:  settingsSvc,
		System:    systemSvc,
		GetSecret: secretLoader.Get,
	}))
	r.Post("/sessions/{sessionID}/reply", replyH.ServeHTTP)
	r.Get("/ws", inprocess.Handler(wsBus, wsRegistry, wsAuthFromQuery(secretLoader.Get, systemSvc)))

	addr := envOr("LISTEN_ADDR", ":8080")
	log.Printf("astroclaw-server listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, r))
}

// wsAuthFromQuery builds an inprocess.AuthFunc that authenticates the
// WebSocket upgrade using a JWT from the ?token= query param, then
// verifies the user is a member of the workspace supplied via
// ?workspace_id=.
func wsAuthFromQuery(getSecret func(context.Context) ([]byte, error), sysSvc *system.Service) inprocess.AuthFunc {
	return func(r *http.Request) (string, string, error) {
		token := strings.TrimSpace(r.URL.Query().Get("token"))
		workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
		if token == "" || workspaceID == "" {
			return "", "", fmt.Errorf("ws auth: missing token or workspace_id")
		}
		secret, err := getSecret(r.Context())
		if err != nil {
			return "", "", fmt.Errorf("ws auth: load JWT secret: %w", err)
		}
		claims, err := auth.Parse(secret, token)
		if err != nil {
			return "", "", fmt.Errorf("ws auth: parse token: %w", err)
		}
		if _, err := sysSvc.GetMembership(r.Context(), claims.UserID, workspaceID); err != nil {
			return "", "", fmt.Errorf("ws auth: not a member: %w", err)
		}
		return claims.UserID, workspaceID, nil
	}
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

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required env %s is not set", key)
	}
	return v
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
