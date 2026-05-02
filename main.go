package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"iclaw/pkg/agent"
	"iclaw/pkg/app/chat"
	"iclaw/pkg/app/notes"
	"iclaw/pkg/app/settings"
	"iclaw/pkg/provider"
	"iclaw/pkg/tool"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Backend abstracts local (direct DB) and remote (HTTP API) modes.
// Local mode uses chat.Service, remote mode uses HTTP calls to the API Gateway.
type Backend interface {
	NewSession(ctx context.Context, userID, title string) (*chat.Session, error)
	ListSessions(ctx context.Context) ([]*chat.Session, error)
	GetSession(ctx context.Context, id string) (*chat.Session, error)
	SoftDeleteSession(ctx context.Context, id string) error
	Reply(ctx context.Context, sessionID, text string) (string, error)
}

// remoteBackend talks to the deployed APIs via HTTP.
// API Gateway handles session CRUD, Function URL handles reply.
type remoteBackend struct {
	apiURL   string // API Gateway URL for session CRUD
	replyURL string // Function URL for reply (no 30s timeout)
	apiKey   string
	client   *http.Client
}

func newRemoteBackend(apiURL, replyURL, apiKey string) *remoteBackend {
	return &remoteBackend{
		apiURL:   strings.TrimRight(apiURL, "/"),
		replyURL: strings.TrimRight(replyURL, "/"),
		apiKey:   apiKey,
		client:   &http.Client{},
	}
}

func (r *remoteBackend) NewSession(ctx context.Context, userID, title string) (*chat.Session, error) {
	body, _ := json.Marshal(map[string]string{"user_id": userID, "title": title})
	resp, err := r.post(ctx, "/sessions", body)
	if err != nil {
		return nil, err
	}
	var s chat.Session
	if err := json.Unmarshal(resp, &s); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &s, nil
}

func (r *remoteBackend) ListSessions(ctx context.Context) ([]*chat.Session, error) {
	resp, err := r.get(ctx, "/sessions")
	if err != nil {
		return nil, err
	}
	var sessions []*chat.Session
	if err := json.Unmarshal(resp, &sessions); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return sessions, nil
}

func (r *remoteBackend) GetSession(ctx context.Context, id string) (*chat.Session, error) {
	resp, err := r.get(ctx, "/sessions/"+id)
	if err != nil {
		return nil, err
	}
	var s chat.Session
	if err := json.Unmarshal(resp, &s); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &s, nil
}

func (r *remoteBackend) SoftDeleteSession(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", r.apiURL+"/sessions/"+id, nil)
	if err != nil {
		return err
	}
	r.setHeaders(req)
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete failed: %s", body)
	}
	return nil
}

// Reply TODO: implement streaming for remote mode. Currently waits for the full
// response before returning, so the user sees no output until it's complete.
func (r *remoteBackend) Reply(ctx context.Context, sessionID, text string) (string, error) {
	body, _ := json.Marshal(map[string]string{"text": text})
	resp, err := r.postTo(ctx, r.replyURL, "/sessions/"+sessionID+"/reply", body)
	if err != nil {
		return "", err
	}
	var result struct {
		Reply string `json:"reply"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	return result.Reply, nil
}

func (r *remoteBackend) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", r.apiURL+path, nil)
	if err != nil {
		return nil, err
	}
	r.setHeaders(req)
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}
	return body, nil
}

func (r *remoteBackend) post(ctx context.Context, path string, body []byte) ([]byte, error) {
	return r.postTo(ctx, r.apiURL, path, body)
}

func (r *remoteBackend) postTo(ctx context.Context, baseURL, path string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	r.setHeaders(req)
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, respBody)
	}
	return respBody, nil
}

func (r *remoteBackend) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if r.apiKey != "" {
		req.Header.Set("x-api-key", r.apiKey)
	}
}

func main() {
	ctx := context.Background()

	var backend Backend

	// Remote mode: connect to deployed APIs.
	// API_URL for session CRUD (API Gateway), REPLY_URL for reply (Function URL).
	if apiURL := os.Getenv("API_URL"); apiURL != "" {
		replyURL := os.Getenv("REPLY_URL")
		if replyURL == "" {
			log.Fatal("REPLY_URL must be set when using remote mode")
		}
		backend = newRemoteBackend(apiURL, replyURL, os.Getenv("API_KEY"))
		fmt.Printf("iClaw (remote: %s) - type /exit to quit\n", apiURL)
	} else {
		// Local mode: connect to local or container database.
		backend = initLocalBackend(ctx)
		fmt.Println("iClaw - type /exit to quit")
	}

	// Create a default session on startup.
	defaultSession, err := backend.NewSession(ctx, "00000000-0000-0000-0000-000000000000", "default")
	if err != nil {
		log.Fatal(err)
	}
	currentSession := defaultSession.ID
	fmt.Printf("Session: %s (%s)\n", defaultSession.Title, defaultSession.ID)

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			return
		}
		input := scanner.Text()
		switch {
		case input == "/exit" || input == "/quit":
			fmt.Println()
			return
		case input == "/sessions":
			sessions, err := backend.ListSessions(ctx)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
				continue
			}
			for _, s := range sessions {
				marker := "  "
				if s.ID == currentSession {
					marker = "* "
				}
				fmt.Printf("%s%s (%s)\n", marker, s.Title, s.ID)
			}
		case strings.HasPrefix(input, "/new"):
			title := strings.TrimSpace(strings.TrimPrefix(input, "/new"))
			if title == "" {
				title = "untitled"
			}
			s, err := backend.NewSession(ctx, "00000000-0000-0000-0000-000000000000", title)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
				continue
			}
			currentSession = s.ID
			fmt.Printf("Created and switched to session: %s (%s)\n", s.Title, s.ID)
		case strings.HasPrefix(input, "/switch "):
			id := strings.TrimSpace(strings.TrimPrefix(input, "/switch "))
			if _, err := backend.GetSession(ctx, id); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
				continue
			}
			currentSession = id
			fmt.Printf("Switched to session: %s\n", id)
		case strings.HasPrefix(input, "/delete "):
			id := strings.TrimSpace(strings.TrimPrefix(input, "/delete "))
			if id == currentSession {
				_, _ = fmt.Fprintln(os.Stderr, "error: cannot delete the active session")
				continue
			}
			if err := backend.SoftDeleteSession(ctx, id); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
				continue
			}
			fmt.Printf("Deleted session: %s\n", id)
		case input == "":
			continue
		default:
			_, err := backend.Reply(ctx, currentSession, input)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
				continue
			}
			// In local mode, OnTextDelta already printed the response
			// token by token. Just add a newline to finish.
			fmt.Println()
		}
	}
}

// initLocalBackend sets up the local mode with database connection,
// LLM provider, tools, and chat service.
func initLocalBackend(ctx context.Context) *chat.Service {
	var p provider.Provider
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		p = provider.NewAnthropic(key, "claude-sonnet-4-20250514")
	} else if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		p = provider.NewOpenAI(key, "gpt-4o-mini")
	} else {
		log.Fatal("OPENAI_API_KEY or ANTHROPIC_API_KEY must be set")
	}

	scanner := bufio.NewScanner(os.Stdin)

	configFn := func(a *agent.Agent) {
		a.OnTextDelta = func(text string) {
			fmt.Print(text)
		}
		a.OnToolCall = func(id string, name string, args string) {
			_, _ = fmt.Fprintf(os.Stderr, "⚡ [%s] calling %s(%s)\n", id, name, args)
		}
		a.OnToolResult = func(id string, name string, result string) {
			_, _ = fmt.Fprintf(os.Stderr, "✓ [%s] %s → %s\n", id, name, result)
		}
		a.OnApproval = func(toolName string, args string) bool {
			_, _ = fmt.Fprintf(os.Stderr, "⚠️  %s(%s)\nApprove? [y/N] ", toolName, args)
			scanner.Scan()
			return strings.ToLower(strings.TrimSpace(scanner.Text())) == "y"
		}
	}

	var pool *pgxpool.Pool
	if connStr := os.Getenv("DATABASE_URL"); connStr != "" {
		var err error
		pool, err = pgxpool.New(ctx, connStr)
		if err != nil {
			log.Fatalf("connect to database: %v", err)
		}
	} else {
		fmt.Println("DATABASE_URL not set, starting temporary PostgreSQL container...")
		migrationFiles, _ := filepath.Glob("migrations/*.sql")
		sort.Strings(migrationFiles)
		if len(migrationFiles) == 0 {
			log.Fatal("no migration files found in migrations/")
		}
		pg, err := postgres.Run(ctx, "postgres:16",
			postgres.WithDatabase("iclaw"),
			postgres.WithUsername("iclaw"),
			postgres.WithPassword("iclaw"),
			postgres.WithInitScripts(migrationFiles...),
			postgres.BasicWaitStrategies(),
			postgres.WithSQLDriver("pgx"),
		)
		if err != nil {
			log.Fatalf("start PostgreSQL container: %v", err)
		}

		connStr, _ := pg.ConnectionString(ctx, "sslmode=disable")
		pool, err = pgxpool.New(ctx, connStr)
		if err != nil {
			log.Fatalf("connect to container database: %v", err)
		}
		fmt.Println("PostgreSQL container ready.")
	}

	// Build system prompt from Settings App + Notes App.
	settingsSvc := settings.NewService(pool)
	notesSvc := notes.NewService(pool)

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
	systemPrompt := agent.BuildSystemPrompt(cfg, agent.DefaultCharLimits())

	// Notes service shared across sessions. MemorySaveTool is created
	// per-session in createFn with the session's ID.
	createFn := func(s *chat.Session) *agent.Agent {
		registry := tool.NewRegistry()
		registry.Register(&tool.TimeTool{})
		registry.Register(&tool.ArithmeticTool{})
		registry.Register(&tool.ReadFileTool{})
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

	return chat.NewService(pool, createFn, configFn)
}
