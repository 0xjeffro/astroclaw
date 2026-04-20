package chat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"iclaw/pkg/agent"
	"iclaw/pkg/provider"
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

// setupService creates a Service with a real PostgresStore for testing.
// Requires DATABASE_URL environment variable or a connection string.
func setupService(connStr string, responses []provider.Message) (*Service, *PostgresStore, *fakeProvider) {
	ps, err := NewPostgresStore(connStr)
	if err != nil {
		panic(fmt.Sprintf("connect to test database: %v", err))
	}
	fp := &fakeProvider{responses: responses}

	createFn := func(s *Session) *agent.Agent {
		return agent.NewFromContext(
			fp, s.SystemPrompt, nil, s.ContextWindow,
			StoreToProviderMessages(s.ContextMessages), s.ContextSummary,
		)
	}

	svc := NewService(ps, createFn, nil)
	return svc, ps, fp
}

// TestReply_PersistsMessages verifies that Reply saves both the user message
// and assistant reply to the Store's message history.
func TestReply_PersistsMessages(t *testing.T) {
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		t.Skip("DATABASE_URL not set")
	}
	svc, ps, _ := setupService(connStr, []provider.Message{
		{Role: "assistant", Content: "hi there"},
	})
	defer func() { _ = ps.Close() }()
	ctx := context.Background()

	s, _ := svc.NewSession(ctx, "chat")
	reply, err := svc.Reply(ctx, s.ID, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if reply != "hi there" {
		t.Errorf("reply: got %q, want %q", reply, "hi there")
	}

	// Check messages were persisted.
	msgs, _ := ps.ListMessages(ctx, s.ID)
	if len(msgs) != 2 {
		t.Fatalf("persisted messages: got %d, want 2 (user + assistant)", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "hello" {
		t.Errorf("msg[0]: got %+v, want user/hello", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "hi there" {
		t.Errorf("msg[1]: got %+v, want assistant/hi there", msgs[1])
	}
}

// TestReply_SavesContext verifies that after Reply, the session's
// ContextMessages and ContextSummary are updated in Store, so the next
// Reply can restore the Agent's state.
func TestReply_SavesContext(t *testing.T) {
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		t.Skip("DATABASE_URL not set")
	}
	svc, ps, _ := setupService(connStr, []provider.Message{
		{Role: "assistant", Content: "reply 1"},
		{Role: "assistant", Content: "reply 2"},
	})
	defer func() { _ = ps.Close() }()
	ctx := context.Background()

	s, _ := svc.NewSession(ctx, "chat")

	// Two rounds of conversation.
	_, _ = svc.Reply(ctx, s.ID, "msg 1")
	_, _ = svc.Reply(ctx, s.ID, "msg 2")

	// Check that session context was updated.
	updated, _ := ps.GetSession(ctx, s.ID)
	if len(updated.ContextMessages) == 0 {
		t.Fatal("ContextMessages should not be empty after Reply")
	}
	// Context should contain the full conversation (user+assistant for each round).
	if len(updated.ContextMessages) != 4 {
		t.Errorf("ContextMessages: got %d, want 4", len(updated.ContextMessages))
	}
}

// TestReply_RestoresContext verifies that creating a new Agent from a session
// with existing ContextMessages correctly restores the conversation state.
// This simulates the "Agent is discarded and rebuilt from Store" scenario.
// The key assertion is that fakeProvider received the restored context
// messages alongside the new user message.
func TestReply_RestoresContext(t *testing.T) {
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		t.Skip("DATABASE_URL not set")
	}
	svc, ps, fp := setupService(connStr, []provider.Message{
		{Role: "assistant", Content: "I remember you, Jeffro"},
	})
	defer func() { _ = ps.Close() }()
	ctx := context.Background()

	// Manually set up a session with existing context (as if from a previous process).
	s := &Session{Title: "restored"}
	_ = ps.CreateSession(ctx, s)
	s.ContextMessages = []Message{
		{Role: "user", Content: "my name is Jeffro"},
		{Role: "assistant", Content: "hello Jeffro"},
	}
	_ = ps.UpdateSession(ctx, s)

	reply, err := svc.Reply(ctx, s.ID, "do you remember me?")
	if err != nil {
		t.Fatal(err)
	}
	if reply != "I remember you, Jeffro" {
		t.Errorf("reply: got %q", reply)
	}

	// Verify the restored context was actually sent to the model.
	// gotMsgs should contain: [user("my name is Jeffro"), assistant("hello Jeffro"), user("do you remember me?")]
	if fp.calls != 1 {
		t.Fatalf("expected 1 Chat call, got %d", fp.calls)
	}
	if len(fp.gotMsgs) < 3 {
		t.Fatalf("expected at least 3 messages sent to model, got %d", len(fp.gotMsgs))
	}
	// Check the restored context is there (not just the new message).
	foundJeffro := false
	for _, m := range fp.gotMsgs {
		if strings.Contains(m.Content, "Jeffro") {
			foundJeffro = true
			break
		}
	}
	if !foundJeffro {
		t.Errorf("model should have received the restored context mentioning 'Jeffro', got: %+v", fp.gotMsgs)
	}
}

// TestReply_InvalidSession verifies that Reply with a non-existent session
// returns an error.
func TestReply_InvalidSession(t *testing.T) {
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		t.Skip("DATABASE_URL not set")
	}
	svc, ps, _ := setupService(connStr, nil)
	defer func() { _ = ps.Close() }()

	_, err := svc.Reply(context.Background(), "no-such-session", "hello")
	if err == nil {
		t.Error("expected error for non-existent session")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}
