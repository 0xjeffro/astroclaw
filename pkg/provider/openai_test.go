package provider

import (
	"context"
	"os"
	"testing"
)

// OPENAI_API_KEY=sk-xxx go test ./pkg/provider/ -v -run TestOpenAIChatStream
func TestOpenAIChatStream(t *testing.T) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	o := NewOpenAI(key, "gpt-4o-mini")
	ch, err := o.ChatStream(context.Background(), []Message{
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

// OPENAI_API_KEY=sk-xxx go test ./pkg/provider/ -v -run TestOpenAIChatSync
func TestOpenAIChatSync(t *testing.T) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	o := NewOpenAI(key, "gpt-4o-mini")
	msg, err := ChatSync(context.Background(), o, []Message{
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
