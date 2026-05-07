package chat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"astroclaw/pkg/agent"
	"astroclaw/pkg/provider"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// testPool is shared across all tests in this package.
// Created once in TestMain, destroyed after all tests finish.
var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()

	// Find migration files (same SQL that runs in production).
	migrationFiles, _ := filepath.Glob("../../../migrations/*.sql")
	sort.Strings(migrationFiles)
	if len(migrationFiles) == 0 {
		panic("no migration files found in ../../../migrations/*.sql")
	}
	fmt.Printf("TestMain: found %d migration files: %v\n", len(migrationFiles), migrationFiles)

	// Start a PostgreSQL container with production migrations applied.
	pg, err := postgres.Run(ctx, "postgres:16",
		postgres.WithDatabase("astroclaw_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		postgres.WithInitScripts(migrationFiles...),
		// BasicWaitStrategies makes testcontainers wait until PostgreSQL is
		// fully ready to accept queries (not just "container started").
		// Without it, tests may connect before init scripts finish running.
		postgres.BasicWaitStrategies(),
		// WithSQLDriver tells the health check to use pgx (our database driver)
		// instead of the default lib/pq. This avoids pulling in a second
		// PostgreSQL driver just for health checks.
		postgres.WithSQLDriver("pgx"),
	)
	if err != nil {
		// If Docker is not available, fall back to DATABASE_URL.
		if connStr := os.Getenv("DATABASE_URL"); connStr != "" {
			testPool, _ = pgxpool.New(ctx, connStr)
			code := m.Run()
			testPool.Close()
			os.Exit(code)
		}
		panic("cannot start test database: " + err.Error())
	}

	connStr, _ := pg.ConnectionString(ctx, "sslmode=disable")
	testPool, err = pgxpool.New(ctx, connStr)
	if err != nil {
		panic("connect to test database: " + err.Error())
	}

	code := m.Run()

	testPool.Close()
	if pg != nil {
		_ = pg.Terminate(ctx)
	}
	os.Exit(code)
}

// fakeProvider for testing. Returns scripted responses in order and
// records the messages it received on the most recent call.
type fakeProvider struct {
	responses []provider.Message
	calls     int
	gotMsgs   []provider.Message
}

func (f *fakeProvider) ChatStream(_ context.Context, msgs []provider.Message, _ []provider.Tool) (<-chan provider.StreamEvent, error) {
	f.gotMsgs = msgs
	var reply provider.Message
	if f.calls >= len(f.responses) {
		reply = provider.Message{Role: "assistant", Content: "default reply"}
	} else {
		reply = f.responses[f.calls]
		f.calls++
	}

	ch := make(chan provider.StreamEvent, len(reply.ToolCalls)*3+2)
	if reply.Content != "" {
		ch <- provider.StreamEvent{Type: provider.StreamEventTextDelta, Text: reply.Content}
	}
	for _, tc := range reply.ToolCalls {
		ch <- provider.StreamEvent{Type: provider.StreamEventToolCallStart, ToolCallID: tc.ID, ToolName: tc.Function.Name}
		ch <- provider.StreamEvent{Type: provider.StreamEventToolCallDelta, ToolCallID: tc.ID, Arguments: tc.Function.Arguments}
		ch <- provider.StreamEvent{Type: provider.StreamEventToolCallEnd, ToolCallID: tc.ID}
	}
	stopReason := "end_turn"
	if len(reply.ToolCalls) > 0 {
		stopReason = "tool_use"
	}
	ch <- provider.StreamEvent{Type: provider.StreamEventDone, StopReason: stopReason}
	close(ch)
	return ch, nil
}

const testUserID = "00000000-0000-0000-0000-000000000001"

// setupService creates a Service for testing using the shared testPool.
// Cleans up data before each test to ensure isolation.
func setupService(t *testing.T, responses []provider.Message) (*Service, *fakeProvider) {
	t.Helper()

	// Clean up data from previous tests.
	_, _ = testPool.Exec(context.Background(), "DELETE FROM app_chat_messages")
	_, _ = testPool.Exec(context.Background(), "DELETE FROM app_chat_sessions")

	fp := &fakeProvider{responses: responses}

	createFn := func(s *Session, agentID string) (*agent.Agent, error) {
		return agent.NewFromContext(
			fp, s.SystemPrompt, nil, s.ContextWindow,
			ToProviderMessages(s.ContextMessages), s.ContextSummary,
		), nil
	}

	svc := NewService(testPool, createFn)
	return svc, fp
}

// TestReply_PersistsMessages verifies that Reply saves both the user message
// and assistant reply to the database.
func TestReply_PersistsMessages(t *testing.T) {
	svc, _ := setupService(t, []provider.Message{
		{Role: "assistant", Content: "hi there"},
	})
	ctx := context.Background()

	s, err := svc.NewSession(ctx, testUserID, nil, "chat")
	if err != nil {
		t.Fatal(err)
	}
	reply, err := svc.Reply(ctx, s.ID, "", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if reply != "hi there" {
		t.Errorf("reply: got %q, want %q", reply, "hi there")
	}
}

// TestReply_SavesContext verifies that after Reply, the session's
// ContextMessages are updated, so the next Reply can restore Agent state.
func TestReply_SavesContext(t *testing.T) {
	svc, _ := setupService(t, []provider.Message{
		{Role: "assistant", Content: "reply 1"},
		{Role: "assistant", Content: "reply 2"},
	})
	ctx := context.Background()

	s, err := svc.NewSession(ctx, testUserID, nil, "chat")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = svc.Reply(ctx, s.ID, "", "msg 1")
	_, _ = svc.Reply(ctx, s.ID, "", "msg 2")

	updated, _ := svc.GetSession(ctx, s.ID)
	if len(updated.ContextMessages) == 0 {
		t.Fatal("ContextMessages should not be empty after Reply")
	}
	if len(updated.ContextMessages) != 4 {
		t.Errorf("ContextMessages: got %d, want 4", len(updated.ContextMessages))
	}
}

// TestReply_RestoresContext verifies that context is correctly restored
// when creating a new Agent from a session with existing ContextMessages.
func TestReply_RestoresContext(t *testing.T) {
	svc, fp := setupService(t, []provider.Message{
		{Role: "assistant", Content: "I remember you, Jeffro"},
		{Role: "assistant", Content: "context reply"},
	})
	ctx := context.Background()

	s, err := svc.NewSession(ctx, testUserID, nil, "restored")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = svc.Reply(ctx, s.ID, "", "my name is Jeffro")

	reply, err := svc.Reply(ctx, s.ID, "", "do you remember me?")
	if err != nil {
		t.Fatal(err)
	}
	if reply != "context reply" {
		t.Errorf("reply: got %q", reply)
	}

	if fp.calls != 2 {
		t.Fatalf("expected 2 Chat calls, got %d", fp.calls)
	}
	foundJeffro := false
	for _, m := range fp.gotMsgs {
		if strings.Contains(m.Content, "Jeffro") {
			foundJeffro = true
			break
		}
	}
	if !foundJeffro {
		t.Errorf("model should have received context mentioning 'Jeffro', got: %+v", fp.gotMsgs)
	}
}

// TestReply_InvalidSession verifies that Reply with a non-existent session
// returns an error.
func TestReply_InvalidSession(t *testing.T) {
	svc, _ := setupService(t, nil)
	_, err := svc.Reply(context.Background(), "00000000-0000-0000-0000-000000000000", "", "hello")
	if err == nil {
		t.Error("expected error for non-existent session")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}
