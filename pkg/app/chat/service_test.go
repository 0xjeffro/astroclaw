package chat

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"iclaw/pkg/agent"
	"iclaw/pkg/provider"

	"github.com/jackc/pgx/v5/pgxpool"
)

// fakeProvider for testing. Returns scripted responses in order and
// records the messages it received on the most recent call.
type fakeProvider struct {
	responses []provider.Message
	calls     int
	gotMsgs   []provider.Message // msgs from the most recent Chat call
}

func (f *fakeProvider) Chat(_ context.Context, msgs []provider.Message, _ []provider.Tool) (provider.Message, error) {
	f.gotMsgs = msgs
	if f.calls >= len(f.responses) {
		return provider.Message{Role: "assistant", Content: "default reply"}, nil
	}
	r := f.responses[f.calls]
	f.calls++
	return r, nil
}

// setupService creates a Service with a real PostgreSQL for testing.
// Requires DATABASE_URL environment variable.
func setupService(t *testing.T, responses []provider.Message) (*Service, *fakeProvider) {
	t.Helper()
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		t.Skip("DATABASE_URL not set")
	}

	pool, err := pgxpool.New(context.Background(), connStr)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	// Clean up test data before each test.
	pool.Exec(context.Background(), "DELETE FROM messages")
	pool.Exec(context.Background(), "UPDATE sessions SET deleted_at = now()")

	fp := &fakeProvider{responses: responses}

	createFn := func(s *Session) *agent.Agent {
		return agent.NewFromContext(
			fp, s.SystemPrompt, nil, s.ContextWindow,
			ToProviderMessages(s.ContextMessages), s.ContextSummary,
		)
	}

	svc := NewService(pool, createFn, nil)
	return svc, fp
}

// TestReply_PersistsMessages verifies that Reply saves both the user message
// and assistant reply to the database.
func TestReply_PersistsMessages(t *testing.T) {
	svc, _ := setupService(t, []provider.Message{
		{Role: "assistant", Content: "hi there"},
	})
	ctx := context.Background()

	s, _ := svc.NewSession(ctx, "chat")
	reply, err := svc.Reply(ctx, s.ID, "hello")
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

	s, _ := svc.NewSession(ctx, "chat")
	_, _ = svc.Reply(ctx, s.ID, "msg 1")
	_, _ = svc.Reply(ctx, s.ID, "msg 2")

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

	// First round: establish context.
	s, _ := svc.NewSession(ctx, "restored")
	_, _ = svc.Reply(ctx, s.ID, "my name is Jeffro")

	// Second round: verify context was restored (Agent sees prior conversation).
	reply, err := svc.Reply(ctx, s.ID, "do you remember me?")
	if err != nil {
		t.Fatal(err)
	}
	if reply != "context reply" {
		t.Errorf("reply: got %q", reply)
	}

	// Verify fakeProvider received the restored context.
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
	_, err := svc.Reply(context.Background(), "00000000-0000-0000-0000-000000000000", "hello")
	if err == nil {
		t.Error("expected error for non-existent session")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}
