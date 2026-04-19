package session

import (
	"context"
	"errors"
	"strings"
	"testing"

	"iclaw/pkg/agent"
	"iclaw/pkg/provider"
	"iclaw/pkg/store"
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

// helper: creates a Manager with MemoryStore and a simple createAgent that
// uses fakeProvider. Returns the manager, store, and fakeProvider.
func setupManager(responses []provider.Message) (*Manager, *store.MemoryStore, *fakeProvider) {
	ms := store.NewMemoryStore()
	fp := &fakeProvider{responses: responses}

	createFn := func(s *store.Session) *agent.Agent {
		return agent.NewFromContext(
			fp, s.SystemPrompt, nil, s.ContextWindow,
			StoreToProviderMessages(s.ContextMessages), s.ContextSummary,
		)
	}

	mgr := NewManager(ms, createFn, nil)
	return mgr, ms, fp
}

// TestNewSession verifies that creating a session stores it and returns
// a valid ID.
func TestNewSession(t *testing.T) {
	mgr, ms, _ := setupManager(nil)
	ctx := context.Background()

	s, err := mgr.NewSession(ctx, "test chat")
	if err != nil {
		t.Fatal(err)
	}
	if s.ID == "" {
		t.Error("session ID should be auto-generated")
	}
	if s.Title != "test chat" {
		t.Errorf("title: got %q, want %q", s.Title, "test chat")
	}

	// Should be in the store.
	got, err := ms.GetSession(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "test chat" {
		t.Errorf("stored title: got %q", got.Title)
	}
}

// TestListSessions verifies listing after creating multiple sessions.
func TestListSessions(t *testing.T) {
	mgr, _, _ := setupManager(nil)
	ctx := context.Background()

	_, _ = mgr.NewSession(ctx, "chat 1")
	_, _ = mgr.NewSession(ctx, "chat 2")

	list, err := mgr.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Errorf("sessions count: got %d, want 2", len(list))
	}
}

// TestGetSession verifies that a created session can be retrieved by ID.
func TestGetSession(t *testing.T) {
	mgr, _, _ := setupManager(nil)
	ctx := context.Background()

	created, _ := mgr.NewSession(ctx, "my chat")
	got, err := mgr.GetSession(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "my chat" {
		t.Errorf("title: got %q, want %q", got.Title, "my chat")
	}
}

// TestGetSession_NotFound verifies that getting a non-existent session
// returns an error. This is what /switch relies on to validate the ID.
func TestGetSession_NotFound(t *testing.T) {
	mgr, _, _ := setupManager(nil)
	_, err := mgr.GetSession(context.Background(), "no-such-id")
	if err == nil {
		t.Error("expected error for non-existent session")
	}
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

// TestDeleteSession verifies that a session can be deleted.
func TestDeleteSession(t *testing.T) {
	mgr, ms, _ := setupManager(nil)
	ctx := context.Background()

	s, _ := mgr.NewSession(ctx, "to delete")
	if err := mgr.DeleteSession(ctx, s.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := ms.GetSession(ctx, s.ID); err == nil {
		t.Error("session should be deleted from store")
	}
}

// TestReply_PersistsMessages verifies that Reply saves both the user message
// and assistant reply to the Store's message history.
func TestReply_PersistsMessages(t *testing.T) {
	mgr, ms, _ := setupManager([]provider.Message{
		{Role: "assistant", Content: "hi there"},
	})
	ctx := context.Background()

	s, _ := mgr.NewSession(ctx, "chat")
	reply, err := mgr.Reply(ctx, s.ID, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if reply != "hi there" {
		t.Errorf("reply: got %q, want %q", reply, "hi there")
	}

	// Check messages were persisted.
	msgs, _ := ms.GetMessages(ctx, s.ID)
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
	mgr, ms, _ := setupManager([]provider.Message{
		{Role: "assistant", Content: "reply 1"},
		{Role: "assistant", Content: "reply 2"},
	})
	ctx := context.Background()

	s, _ := mgr.NewSession(ctx, "chat")

	// Two rounds of conversation.
	_, _ = mgr.Reply(ctx, s.ID, "msg 1")
	_, _ = mgr.Reply(ctx, s.ID, "msg 2")

	// Check that session context was updated.
	updated, _ := ms.GetSession(ctx, s.ID)
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
	mgr, ms, fp := setupManager([]provider.Message{
		{Role: "assistant", Content: "I remember you, Jeffro"},
	})
	ctx := context.Background()

	// Manually set up a session with existing context (as if from a previous process).
	s := &store.Session{Title: "restored"}
	_ = ms.CreateSession(ctx, s)
	s.ContextMessages = []store.Message{
		{Role: "user", Content: "my name is Jeffro"},
		{Role: "assistant", Content: "hello Jeffro"},
	}
	_ = ms.UpdateSession(ctx, s)

	reply, err := mgr.Reply(ctx, s.ID, "do you remember me?")
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
	mgr, _, _ := setupManager(nil)
	_, err := mgr.Reply(context.Background(), "no-such-session", "hello")
	if err == nil {
		t.Error("expected error for non-existent session")
	}
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}
