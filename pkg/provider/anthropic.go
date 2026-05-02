package provider

// Anthropic Claude provider implementation.
//
// API reference:
//   - Messages API: https://docs.anthropic.com/en/api/messages
//   - Tool Use:     https://docs.anthropic.com/en/docs/build-with-claude/tool-use

import (
	"context"
	"encoding/json"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

type Anthropic struct {
	client anthropic.Client
	model  string
}

func NewAnthropic(apiKey, model string) *Anthropic {
	client := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &Anthropic{client: client, model: model}
}

func (a *Anthropic) ChatStream(ctx context.Context, msgs []Message, tools []Tool) (<-chan StreamEvent, error) {
	// Extract system messages into a top-level "system" field, since
	// Anthropic doesn't accept `system` as a message role.
	system, chatMsgs := extractSystem(msgs)

	// Convert our Message format → Anthropic's SDK Message.
	params := anthropic.MessageNewParams{
		Model: a.model,
		// TODO: make this configurable per model. Different models have different
		// maximum values (e.g. Claude Sonnet 16384, Haiku 4096).
		// https://docs.claude.com/en/docs/models-overview
		MaxTokens: 4096,
		Messages:  toSDKMessages(chatMsgs),
	}
	if system != "" {
		params.System = []anthropic.TextBlockParam{
			{Text: system},
		}
	}
	if len(tools) > 0 {
		params.Tools = toSDKTools(tools)
	}
	stream := a.client.Messages.NewStreaming(ctx, params)

	ch := make(chan StreamEvent)
	go func() {
		defer close(ch)
		defer func() { _ = stream.Close() }()

		// send pushes an event to the channel or returns false if the
		// context was cancelled (e.g. user disconnected). This prevents
		// the goroutine from blocking forever on a full channel.
		send := func(e StreamEvent) bool {
			select {
			case ch <- e:
				return true
			case <-ctx.Done():
				return false
			}
		}

		var currentToolCallID string

		for stream.Next() {
			event := stream.Current()
			switch variant := event.AsAny().(type) {
			case anthropic.ContentBlockStartEvent:
				switch block := variant.ContentBlock.AsAny().(type) {
				case anthropic.ToolUseBlock:
					currentToolCallID = block.ID
					if !send(StreamEvent{
						Type:       StreamEventToolCallStart,
						ToolCallID: block.ID,
						ToolName:   block.Name,
					}) {
						return
					}
				case anthropic.TextBlock:
					// Text content arrives via TextDelta in ContentBlockDeltaEvent.
				case anthropic.ThinkingBlock:
					// Thinking content arrives via ThinkingDelta in ContentBlockDeltaEvent.

					// Anthropic server tools (web_search, web_fetch, code_execution) produce
					// additional block types (ServerToolUseBlock, WebSearchToolResultBlock, etc.)
					// that are not handled here. Bedrock does not support server tools, so we
					// skip them for now to keep Bedrock compatibility.
					// https://docs.anthropic.com/en/docs/agents-and-tools/tool-use/overview#server-tools
				}

			case anthropic.ContentBlockDeltaEvent:
				switch delta := variant.Delta.AsAny().(type) {
				case anthropic.TextDelta:
					if !send(StreamEvent{Type: StreamEventTextDelta, Text: delta.Text}) {
						return
					}
				case anthropic.InputJSONDelta:
					if !send(StreamEvent{
						Type:       StreamEventToolCallDelta,
						ToolCallID: currentToolCallID,
						Arguments:  delta.PartialJSON,
					}) {
						return
					}
				case anthropic.ThinkingDelta:
					if !send(StreamEvent{Type: StreamEventThinkingDelta, Thinking: delta.Thinking}) {
						return
					}
				}
			case anthropic.ContentBlockStopEvent:
				if currentToolCallID != "" {
					if !send(StreamEvent{Type: StreamEventToolCallEnd, ToolCallID: currentToolCallID}) {
						return
					}
					currentToolCallID = ""
				}
			case anthropic.MessageDeltaEvent:
				raw := string(variant.Delta.StopReason)
				stopReason := raw // pass through unrecognized values as-is
				switch raw {
				case "end_turn":
					stopReason = StopReasonEndTurn
				case "tool_use":
					stopReason = StopReasonToolUse
				case "max_tokens":
					stopReason = StopReasonMaxTokens
				}
				if !send(StreamEvent{Type: StreamEventDone, StopReason: stopReason}) {
					return
				}
			}
		}
		if err := stream.Err(); err != nil {
			send(StreamEvent{Type: StreamEventError, Error: err.Error()})
		}
	}()

	return ch, nil
}

// extractSystem pulls out system messages from the msg slice and returns
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

// toSDKMessages converts iclaw Messages to Anthropic SDK MessageParam slice.
// Key differences from iclaw's format:
//   - Content is an array of typed blocks, not a plain string
//   - Tool calls are ToolUseBlock inside assistant message content
//   - Tool results must be inside a user message; consecutive tool results
//     are merged into a single user message (Anthropic requirement)
func toSDKMessages(msgs []Message) []anthropic.MessageParam {
	var out []anthropic.MessageParam
	for _, m := range msgs {
		switch {
		case m.Role == "tool":
			block := anthropic.NewToolResultBlock(m.ToolCallID, m.Content, false)
			// Merge consecutive tool results into the same user message.
			if len(out) > 0 && out[len(out)-1].Role == "user" {
				out[len(out)-1].Content = append(out[len(out)-1].Content, block)
			} else {
				out = append(out, anthropic.NewUserMessage(block))
			}

		case m.Role == "assistant" && len(m.ToolCalls) > 0:
			var blocks []anthropic.ContentBlockParamUnion
			if m.Content != "" {
				blocks = append(blocks, anthropic.NewTextBlock(m.Content))
			}
			for _, tc := range m.ToolCalls {
				var input any
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
					input = map[string]any{}
				}
				blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, input, tc.Function.Name))
			}
			out = append(out, anthropic.MessageParam{Role: "assistant", Content: blocks})

		case m.Role == "user":
			out = append(out, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))

		case m.Role == "assistant":
			out = append(out, anthropic.NewAssistantMessage(anthropic.NewTextBlock(m.Content)))
		}
	}
	return out
}

// toSDKTools converts iclaw Tools to Anthropic SDK ToolUnionParam slice.
func toSDKTools(tools []Tool) []anthropic.ToolUnionParam {
	var out []anthropic.ToolUnionParam
	for _, t := range tools {
		out = append(out, anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        t.Function.Name,
				Description: anthropic.String(t.Function.Description),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: t.Function.Parameters["properties"],
					Required:   toStringSlice(t.Function.Parameters["required"]),
				},
			},
		})
	}
	return out
}

// toStringSlice converts an any value (expected to be []string or []any)
// to []string. Returns nil if conversion fails.
func toStringSlice(v any) []string {
	if v == nil {
		return nil
	}
	if ss, ok := v.([]string); ok {
		return ss
	}
	if arr, ok := v.([]any); ok {
		var out []string
		for _, item := range arr {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
