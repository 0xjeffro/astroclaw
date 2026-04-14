package provider

// Anthropic Claude provider implementation.
//
// API reference:
//   - Messages API: https://docs.anthropic.com/en/api/messages
//   - Tool Use:     https://docs.anthropic.com/en/docs/build-with-claude/tool-use

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Anthropic struct {
	APIKey  string
	BaseURL string
	Model   string
	HTTP    *http.Client
}

func NewAnthropic(apiKey, model string) *Anthropic {
	return &Anthropic{
		APIKey:  apiKey,
		BaseURL: "https://api.anthropic.com",
		Model:   model,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

func (a *Anthropic) Chat(ctx context.Context, msgs []Message, tools []Tool) (Message, error) {

	// Extract system messages into a top-level "system" field, since
	// Anthropic doesn't accept `system` as a message role.
	system, chatMsgs := extractSystem(msgs)

	// Convert our Message format → Anthropic's content-block-based format.
	anthropicMsgs := toAnthropicMessages(chatMsgs)

	// Convert out Tool format -> Anthropic tool format
	anthropicTools := toAnthropicTools(tools)

	// Build request body
	body := map[string]any{
		"model":      a.Model,
		"max_tokens": 4096,
		"messages":   anthropicMsgs,
	}
	if system != "" {
		body["system"] = system
	}
	if len(anthropicTools) > 0 {
		body["tools"] = anthropicTools
	}

	reqBody, err := json.Marshal(body)
	if err != nil {
		return Message{}, fmt.Errorf("failed to marshal anthropic request body: %w", err)
	}

	// Send HTTP request.
	req, err := http.NewRequestWithContext(ctx, "POST", a.BaseURL+"/v1/messages", bytes.NewReader(reqBody))
	if err != nil {
		return Message{}, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.APIKey)             // Anthropic uses x-api-key, not Authorization Bearer
	req.Header.Set("anthropic-version", "2023-06-01") // Required version header

	resp, err := a.HTTP.Do(req)
	if err != nil {
		return Message{}, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return Message{}, fmt.Errorf("anthropic request failed with status code %d: %s", resp.StatusCode, string(errBody))
	}

	// Parse Anthropic response -> our Message format
	return parseAnthropicResponse(resp.Body)
}

// extractSystem pulls out system messages from the msgs slice and returns
// the concatenated system prompt + the remaining non-system messages.
func extractSystem(msgs []Message) (string, []Message) {
	var system string
	var chatMsgs []Message
	for _, m := range msgs {
		if m.Role == "system" {
			if system != "" {
				system += "\n\n"
			}
			system += m.Content
		} else {
			chatMsgs = append(chatMsgs, m)
		}
	}
	return system, chatMsgs
}

// toAnthropicMessages converts our Message slice into Anthropic's message
// format. Key differences:
//   - content is an array of blocks, not a plain string
//   - tool calls are {type:"tool_use"} blocks inside content
//   - tool results are {role:"user"} with {type:"tool_result"} blocks
func toAnthropicMessages(msgs []Message) []map[string]any {
	var out []map[string]any
	for _, m := range msgs {
		switch {
		// Case 1: Tool result.
		// Ours:      {role:"tool", tool_call_id:"xxx", content:"result"}
		// Anthropic: {role:"user", content:[{type:"tool_result", tool_use_id:"xxx", content:"result"}]}
		case m.Role == "tool":
			out = appendToolResult(out, m)

		// Case 2: Assistant message containing tool calls.
		// Ours:      {role:"assistant", content:"", tool_calls:[{id, function:{name, arguments}}]}
		// Anthropic: {role:"assistant", content:[{type:"tool_use", id, name, input}]}
		case m.Role == "assistant" && len(m.ToolCalls) > 0:
			out = append(out, buildAssistantWithToolCalls(m))

		// Case 3: Plain user or assistant message (text only).
		// Ours:      {role:"user", content:"hello"}
		// Anthropic: {role:"user", content:[{type:"text", text:"hello"}]}
		default:
			out = append(out, map[string]any{
				"role": m.Role,
				"content": []map[string]any{
					{"type": "text", "text": m.Content},
				},
			})
		}
	}

	return out
}

// appendToolResult handles converting {role:"tool"} messages to Anthropic format.
// Anthropic requires tool results to be inside a {role:"user"} message.
// If the previous message is already a user message (from an earlier tool result in the same batch),
// we merge into it. Otherwise, we create a new user message.
func appendToolResult(out []map[string]any, m Message) []map[string]any {
	block := map[string]any{
		"type":        "tool_result",
		"tool_use_id": m.ToolCallID,
		"content":     m.Content,
	}

	// Try to merge into the previous user message (multiple tool results
	// from parallel tool calls should be in the same user message).
	if len(out) > 0 {
		prev := out[len(out)-1]
		if prev["role"] == "user" {
			if content, ok := prev["content"].([]map[string]any); ok {
				prev["content"] = append(content, block)
				return out
			}
		}
	}

	return append(out, map[string]any{
		"role":    "user",
		"content": []map[string]any{block},
	})
}

func buildAssistantWithToolCalls(m Message) map[string]any {
	var content []map[string]any

	if m.Content != "" {
		content = append(content, map[string]any{
			"type": "text",
			"text": m.Content,
		})
	}

	for _, tc := range m.ToolCalls {
		// Parse arguments string back to JSON object — Anthropic expects
		// input as a JSON object, not a string like OpenAI.
		var input any
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
			input = map[string]any{}
		}

		content = append(content, map[string]any{
			"type":  "tool_use",
			"id":    tc.ID,
			"name":  tc.Function.Name,
			"input": input,
		})
	}

	return map[string]any{
		"role":    "assistant",
		"content": content,
	}
}

// toAnthropicTools converts our Tool format → Anthropic tool format.
// Anthropic uses {name, description, input_schema} (flat structure),
// unlike OpenAI's {type:"function", function:{name, description, parameters}}.
func toAnthropicTools(tools []Tool) []map[string]any {
	if len(tools) == 0 {
		return nil
	}
	var out []map[string]any
	for _, t := range tools {
		out = append(out, map[string]any{
			"name":         t.Function.Name,
			"description":  t.Function.Description,
			"input_schema": t.Function.Parameters,
		})
	}
	return out
}

// parseAnthropicResponse reads the Anthropic response body and converts
// it into our Message format. Anthropic's response.content is an array of blocks,
// we extract text blocks into Content and tool_use blocks into ToolCalls.
func parseAnthropicResponse(body io.Reader) (Message, error) {
	var resp struct {
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`  // for type=="text"
			ID    string          `json:"id"`    // for type=="tool_use"
			Name  string          `json:"name"`  // for type=="tool_use"
			Input json.RawMessage `json:"input"` // for type=="tool_use"
		} `json:"content"`
		StopReason string `json:"stop_reason"`
	}

	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		return Message{}, fmt.Errorf("decode response: %w", err)
	}

	var msg Message
	msg.Role = "assistant"

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			msg.Content += block.Text
		case "tool_use":
			// Convert input (JSON object) back to string for our ToolCall format.
			argsStr := "{}"
			if len(block.Input) > 0 {
				argsStr = string(block.Input)
			}

			msg.ToolCalls = append(msg.ToolCalls, ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: ToolCallFunc{
					Name:      block.Name,
					Arguments: argsStr,
				},
			})
		}
	}

	return msg, nil
}
