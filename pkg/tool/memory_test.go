package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// fakeMemoryStore implements MemoryStore for testing.
type fakeMemoryStore struct {
	saved     []string
	shouldErr bool
}

func (f *fakeMemoryStore) SaveMemory(_ context.Context, agentID, content, sessionID string, messageIDs []string) error {
	if f.shouldErr {
		return fmt.Errorf("store error")
	}
	f.saved = append(f.saved, content)
	return nil
}

func makeMemoryArgs(content string) string {
	b, _ := json.Marshal(map[string]string{"content": content})
	return string(b)
}

// Verifies that a valid memory is saved and the tool returns success.
func TestMemorySave_Success(t *testing.T) {
	store := &fakeMemoryStore{}
	tool := &MemorySaveTool{Store: store, SessionID: "session-1"}

	result, err := tool.Execute(context.Background(), makeMemoryArgs("User prefers Go"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "memory saved" {
		t.Errorf("got %q, want %q", result, "memory saved")
	}
	if len(store.saved) != 1 || store.saved[0] != "User prefers Go" {
		t.Errorf("store should have one entry 'User prefers Go', got %v", store.saved)
	}
}

// Verifies that empty content returns an error.
func TestMemorySave_EmptyContent(t *testing.T) {
	store := &fakeMemoryStore{}
	tool := &MemorySaveTool{Store: store, SessionID: "session-1"}

	_, err := tool.Execute(context.Background(), makeMemoryArgs(""))
	if err == nil {
		t.Fatal("expected error for empty content")
	}
	if !strings.Contains(err.Error(), "content cannot be empty") {
		t.Errorf("error %q should mention 'content cannot be empty'", err.Error())
	}
}

// Verifies that bad JSON arguments return an error.
func TestMemorySave_BadJSON(t *testing.T) {
	store := &fakeMemoryStore{}
	tool := &MemorySaveTool{Store: store, SessionID: "session-1"}

	_, err := tool.Execute(context.Background(), "not json")
	if err == nil {
		t.Fatal("expected error for bad JSON")
	}
	if !strings.Contains(err.Error(), "bad args") {
		t.Errorf("error %q should contain 'bad args'", err.Error())
	}
}

// Verifies that store errors propagate to the caller.
func TestMemorySave_StoreError(t *testing.T) {
	store := &fakeMemoryStore{shouldErr: true}
	tool := &MemorySaveTool{Store: store, SessionID: "session-1"}

	_, err := tool.Execute(context.Background(), makeMemoryArgs("something"))
	if err == nil {
		t.Fatal("expected error when store fails")
	}
	if !strings.Contains(err.Error(), "save memory") {
		t.Errorf("error %q should contain 'save memory'", err.Error())
	}
}
