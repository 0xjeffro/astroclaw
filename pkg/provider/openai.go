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

func (o *OpenAI) Chat(ctx context.Context, msgs []Message, tools []Tool) (Message, error) {
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

	resp, err := o.HTTP.Do(req)
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
