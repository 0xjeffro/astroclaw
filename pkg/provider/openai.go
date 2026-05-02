package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type OpenAI struct {
	APIKey  string
	BaseURL string // https://api.openai.com/v1 by default
	Model   string
	HTTP    *http.Client
}

func NewOpenAI(apiKey, model string) *OpenAI {
	return &OpenAI{
		APIKey:  apiKey,
		BaseURL: "https://api.openai.com/v1",
		Model:   model,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

// TODO: rewrite with OpenAI Go SDK and real streaming support.
// This is a placeholder that wraps the old synchronous Chat into a channel.
func (o *OpenAI) ChatStream(ctx context.Context, msgs []Message, tools []Tool) (<-chan StreamEvent, error) {
	msg, err := o.chat(ctx, msgs, tools)
	if err != nil {
		return nil, err
	}
	ch := make(chan StreamEvent, len(msg.ToolCalls)*3+2)
	if msg.Content != "" {
		ch <- StreamEvent{Type: StreamEventTextDelta, Text: msg.Content}
	}
	for _, tc := range msg.ToolCalls {
		ch <- StreamEvent{Type: StreamEventToolCallStart, ToolCallID: tc.ID, ToolName: tc.Function.Name}
		ch <- StreamEvent{Type: StreamEventToolCallDelta, ToolCallID: tc.ID, Arguments: tc.Function.Arguments}
		ch <- StreamEvent{Type: StreamEventToolCallEnd, ToolCallID: tc.ID}
	}
	stopReason := "end_turn"
	if len(msg.ToolCalls) > 0 {
		stopReason = "tool_use"
	}
	ch <- StreamEvent{Type: StreamEventDone, StopReason: stopReason}
	close(ch)
	return ch, nil
}

func (o *OpenAI) chat(ctx context.Context, msgs []Message, tools []Tool) (Message, error) {
	body := map[string]any{
		"model":    o.Model,
		"messages": msgs,
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	reqBody, err := json.Marshal(body)
	if err != nil {
		return Message{}, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", o.BaseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return Message{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.APIKey)

	resp, err := doWithRetry(o.HTTP, req, reqBody)
	if err != nil {
		return Message{}, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return Message{}, fmt.Errorf("openai: status %d: %s", resp.StatusCode, string(errBody))
	}

	var out struct {
		Choices []struct {
			Message struct {
				Role      string     `json:"role"`
				Content   string     `json:"content"`
				ToolCalls []ToolCall `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Message{}, fmt.Errorf("decode response: %w", err)
	}
	if len(out.Choices) == 0 {
		return Message{}, fmt.Errorf("openai: empty choices")
	}
	m := out.Choices[0].Message
	return Message{
		Role:      m.Role,
		Content:   m.Content,
		ToolCalls: m.ToolCalls,
	}, nil
}
