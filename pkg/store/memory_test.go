package store

import (
	"context"
	"errors"
	"testing"
)

func TestCreateAndGetSession(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()

	session := &Session{Title: "test session", UserID: "u1"}
	if err := s.CreateSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	if session.ID == "" {
		t.Error("session ID should be auto-generated")
	}
	if session.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}

	got, err := s.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "test session" {
		t.Errorf("title: got %q, want %q", got.Title, "test session")
	}
}

// TestGetSession_NotFound verifies that fetching a non-existent session
// returns an error.
func TestGetSession_NotFound(t *testing.T) {
	s := NewMemoryStore()
	_, err := s.GetSession(context.Background(), "no-such-id")
	if err == nil {
		t.Error("expected error for non-existent session")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

// TestListSessions verifies listing sessions, with and without userID filter.
func TestListSessions(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()

	_ = s.CreateSession(ctx, &Session{Title: "s1", UserID: "u1"})
	_ = s.CreateSession(ctx, &Session{Title: "s2", UserID: "u1"})
	_ = s.CreateSession(ctx, &Session{Title: "s3", UserID: "u2"})

	// List all.
	all, _ := s.ListSessions(ctx, "")
	if len(all) != 3 {
		t.Errorf("list all: got %d, want 3", len(all))
	}

	// List by userID.
	u1Sessions, _ := s.ListSessions(ctx, "u1")
	if len(u1Sessions) != 2 {
		t.Errorf("list u1: got %d, want 2", len(u1Sessions))
	}
}

// TestDeleteSession verifies that deleting a session removes it and its messages.
func TestDeleteSession(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()

	session := &Session{Title: "to delete"}
	_ = s.CreateSession(ctx, session)
	_ = s.CreateMessage(ctx, &Message{SessionID: session.ID, Role: "user", Content: "hello"})

	if err := s.DeleteSession(ctx, session.ID); err != nil {
		t.Fatal(err)
	}

	// Session should be gone.
	if _, err := s.GetSession(ctx, session.ID); err == nil {
		t.Error("session should be deleted")
	}

	// Messages should be gone too.
	if _, err := s.ListMessages(ctx, session.ID); err == nil {
		t.Error("messages should be deleted with session")
	}
}

// TestDeleteSession_NotFound verifies that deleting a non-existent session
// returns an error.
func TestDeleteSession_NotFound(t *testing.T) {
	s := NewMemoryStore()
	err := s.DeleteSession(context.Background(), "no-such-id")
	if err == nil {
		t.Error("expected error for non-existent session")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

// TestAppendAndGetMessages verifies appending messages to a session and
// retrieving them in order.
func TestAppendAndGetMessages(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()

	session := &Session{Title: "chat"}
	_ = s.CreateSession(ctx, session)

	_ = s.CreateMessage(ctx, &Message{SessionID: session.ID, Role: "user", Content: "hello"})
	_ = s.CreateMessage(ctx, &Message{SessionID: session.ID, Role: "assistant", Content: "hi"})
	_ = s.CreateMessage(ctx, &Message{SessionID: session.ID, Role: "user", Content: "bye"})

	msgs, err := s.ListMessages(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("messages count: got %d, want 3", len(msgs))
	}

	// Check sequence numbers are auto-assigned and increasing.
	for i, m := range msgs {
		if m.SequenceNumber != i+1 {
			t.Errorf("msg[%d] seq: got %d, want %d", i, m.SequenceNumber, i+1)
		}
		if m.ID == "" {
			t.Errorf("msg[%d] ID should be auto-generated", i)
		}
	}

	// Check content preserved.
	if msgs[0].Content != "hello" || msgs[1].Content != "hi" || msgs[2].Content != "bye" {
		t.Errorf("message content mismatch: %+v", msgs)
	}
}

// TestAppendMessage_NoSession verifies that appending to a non-existent
// session returns an error.
func TestAppendMessage_NoSession(t *testing.T) {
	s := NewMemoryStore()
	err := s.CreateMessage(context.Background(), &Message{SessionID: "no-such", Role: "user", Content: "hi"})
	if err == nil {
		t.Error("expected error for non-existent session")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

// TestAppendMessages_Batch verifies that AppendMessages writes multiple
// messages in one call with correct sequence numbers.
func TestAppendMessages_Batch(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()

	session := &Session{Title: "chat"}
	_ = s.CreateSession(ctx, session)

	msgs := []Message{
		{SessionID: session.ID, Role: "user", Content: "hello"},
		{SessionID: session.ID, Role: "assistant", Content: "hi"},
		{SessionID: session.ID, Role: "user", Content: "bye"},
	}
	if err := s.CreateMessages(ctx, msgs); err != nil {
		t.Fatal(err)
	}

	got, _ := s.ListMessages(ctx, session.ID)
	if len(got) != 3 {
		t.Fatalf("messages count: got %d, want 3", len(got))
	}
	// Sequence numbers should be auto-assigned: 1, 2, 3.
	for i, m := range got {
		if m.SequenceNumber != i+1 {
			t.Errorf("msg[%d] seq: got %d, want %d", i, m.SequenceNumber, i+1)
		}
	}
}

// TestUpdateSession verifies that UpdateSession persists changes to session
// fields including ContextMessages and ContextSummary.
func TestUpdateSession(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()

	session := &Session{Title: "original"}
	_ = s.CreateSession(ctx, session)

	// Update title, context summary, and context messages.
	session.Title = "updated"
	session.ContextSummary = "user discussed goroutines"
	session.ContextMessages = []Message{
		{Role: "user", Content: "turn3"},
		{Role: "assistant", Content: "reply3"},
	}
	if err := s.UpdateSession(ctx, session); err != nil {
		t.Fatal(err)
	}

	got, _ := s.GetSession(ctx, session.ID)
	if got.Title != "updated" {
		t.Errorf("title: got %q, want %q", got.Title, "updated")
	}
	if got.ContextSummary != "user discussed goroutines" {
		t.Errorf("summary: got %q", got.ContextSummary)
	}
	if len(got.ContextMessages) != 2 {
		t.Fatalf("context messages: got %d, want 2", len(got.ContextMessages))
	}
	if got.ContextMessages[0].Content != "turn3" {
		t.Errorf("first context msg: got %q, want %q", got.ContextMessages[0].Content, "turn3")
	}
	if got.UpdatedAt.After(got.CreatedAt) == false {
		t.Error("UpdatedAt should be after CreatedAt")
	}
}

// TestUpdateSession_NotFound verifies that updating a non-existent session
// returns an error.
func TestUpdateSession_NotFound(t *testing.T) {
	s := NewMemoryStore()
	err := s.UpdateSession(context.Background(), &Session{ID: "no-such"})
	if err == nil {
		t.Error("expected error for non-existent session")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}
