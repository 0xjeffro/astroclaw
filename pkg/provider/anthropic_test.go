package provider

import (
	"context"
	"os"
	"testing"
)

// ANTHROPIC_API_KEY=sk-ant-xxx go test ./pkg/provider/ -v -run TestAnthropicChat
func TestAnthropicChat(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	a := NewAnthropic(key, "claude-sonnet-4-20250514")
	got, err := a.Chat(context.Background(), []Message{
		{Role: "user", Content: "say hi in 3 words"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("got: %+v", got)
}
