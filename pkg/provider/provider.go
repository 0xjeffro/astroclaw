package provider

import (
	"context"
	"fmt"
)

// Message represents a single message in the conversation history.
// Role determines the message type and how each Provider translates it:
//
//   - "System": System prompt. OpenAI sends it as a message in the array;
//     Anthropic extracts it into a top-level "system" field.
//   - "User": User input. Sent as-is to all providers.
//   - "Assistant": Model reply. If ToolCalls is non-empty, the model is
//     requesting tool execution rather than giving a final answer.
//   - "Tool": Tool execution result. ToolCallID must match the ID from
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
	ChatStream(ctx context.Context, msgs []Message, tools []Tool) (<-chan StreamEvent, error)
}

// ChatSync consumes a ChatStream channel and assembles the events into a complete Message.
// Use this when streaming output is not needed (e.g., wen summarization we don't need streaming).
func ChatSync(ctx context.Context, p Provider, msgs []Message, tools []Tool) (Message, error) {
	ch, err := p.ChatStream(ctx, msgs, tools)
	if err != nil {
		return Message{}, err
	}

	var msg Message
	msg.Role = "assistant"

	// An LLM response may contain multiple tool calls. Each tool call's
	// arguments arrive as a series of JSON fragments (tool_call_delta events).
	// toolCalls stores ToolCall structs in the order they started.
	// toolArgs accumulates the JSON fragments per tool call ID until tool_call_end.
	var toolCalls []ToolCall
	toolArgs := map[string]string{}

	for event := range ch {
		switch event.Type {
		case StreamEventTextDelta:
			msg.Content += event.Text
		case StreamEventToolCallStart:
			toolCalls = append(toolCalls, ToolCall{
				ID:   event.ToolCallID,
				Type: "function",
				Function: ToolCallFunc{
					Name: event.ToolName,
				},
			})
			toolArgs[event.ToolCallID] = ""
		case StreamEventToolCallDelta:
			toolArgs[event.ToolCallID] += event.Arguments
		case StreamEventToolCallEnd:
			for i := range toolCalls {
				if toolCalls[i].ID == event.ToolCallID {
					toolCalls[i].Function.Arguments = toolArgs[event.ToolCallID]
					break
				}
			}
		case StreamEventError:
			return Message{}, fmt.Errorf("stream error: %s", event.Error)
		}
	}

	msg.ToolCalls = toolCalls
	return msg, nil
}
