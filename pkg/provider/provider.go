package provider

import "context"

// Message represents a single message in the conversation history.
// Role determines the message type and how each Provider translates it:
//
//   - "system":    System prompt. OpenAI sends it as a message in the array;
//     Anthropic extracts it into a top-level "system" field.
//   - "user":      User input. Sent as-is to all providers.
//   - "assistant": Model reply. If ToolCalls is non-empty, the model is
//     requesting tool execution rather than giving a final answer.
//   - "tool":      Tool execution result. ToolCallID must match the ID from
//     the corresponding ToolCall. OpenAI uses {role:"tool"};
//     Anthropic wraps it as {role:"user", content:[{type:"tool_result",...}]}.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`

	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`   // optional tool calls made by the model in this message
	ToolCallID string     `json:"tool_call_id,omitempty"` // only for role:"tool"
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // "function"
	Function ToolCallFunc `json:"function"`
}

type ToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type Provider interface {
	Chat(ctx context.Context, msgs []Message, tools []Tool) (Message, error)
}
