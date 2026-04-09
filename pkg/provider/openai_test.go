package provider

import (
	"context"
	"os"
	"testing"
)

// OPENAI_API_KEY=sk-xxx go test ./pkg/provider/ -v -run TestOpenAIChat
func TestOpenAIChat(t *testing.T) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	o := NewOpenAI(key, "gpt-4o-mini")
	got, err := o.Chat(context.Background(), []Message{
		{Role: "user", Content: "say hi in 3 words"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("got: %+v", got)
}
