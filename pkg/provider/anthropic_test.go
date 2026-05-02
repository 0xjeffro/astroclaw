package provider

import (
	"context"
	"os"
	"testing"
)

// ANTHROPIC_API_KEY=sk-ant-xxx go test ./pkg/provider/ -v -run TestAnthropicChatStream
func TestAnthropicChatStream(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	a := NewAnthropic(key, "claude-sonnet-4-20250514")
	ch, err := a.ChatStream(context.Background(), []Message{
		{Role: "user", Content: "say hi in 3 words"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	var fullText string
	for event := range ch {
		switch event.Type {
		case StreamEventTextDelta:
			fullText += event.Text
			t.Logf("text_delta: %q", event.Text)
		case StreamEventDone:
			t.Logf("done: stop_reason=%s", event.StopReason)
		case StreamEventError:
			t.Fatalf("stream error: %s", event.Error)
		}
	}

	if fullText == "" {
		t.Error("expected non-empty response")
	}
	t.Logf("full response: %q", fullText)
}

// ANTHROPIC_API_KEY=sk-ant-xxx go test ./pkg/provider/ -v -run TestAnthropicChatSync
func TestAnthropicChatSync(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	a := NewAnthropic(key, "claude-sonnet-4-20250514")
	msg, err := ChatSync(context.Background(), a, []Message{
		{Role: "user", Content: "say hi in 3 words"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content == "" {
		t.Error("expected non-empty content")
	}
	t.Logf("got: %+v", msg)
}
