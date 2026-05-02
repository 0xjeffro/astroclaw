package provider

import (
	"context"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
)

type OpenAI struct {
	client openai.Client
	model  string
}

func NewOpenAI(apiKey, model string) *OpenAI {
	client := openai.NewClient(option.WithAPIKey(apiKey))
	return &OpenAI{client: client, model: model}
}

func (o *OpenAI) ChatStream(ctx context.Context, msgs []Message, tools []Tool) (<-chan StreamEvent, error) {
	params := openai.ChatCompletionNewParams{
		Model:    o.model,
		Messages: toOpenAIMessages(msgs),
	}
	if len(tools) > 0 {
		params.Tools = toOpenAITools(tools)
	}

	stream := o.client.Chat.Completions.NewStreaming(ctx, params)
	ch := make(chan StreamEvent)

	go func() {
		defer close(ch)
		defer func() { _ = stream.Close() }()

		send := func(e StreamEvent) bool {
			select {
			case ch <- e:
				return true
			case <-ctx.Done():
				return false
			}
		}

		// Track tool calls by index. OpenAI sends tool call ID and name
		// in the first chunk, then arguments in subsequent chunks.
		type toolCallState struct {
			id   string
			name string
		}
		toolCalls := map[int64]*toolCallState{}

		for stream.Next() {
			chunk := stream.Current()
			if len(chunk.Choices) == 0 {
				continue
			}
			choice := chunk.Choices[0]
			delta := choice.Delta

			// Text delta
			if delta.Content != "" {
				if !send(StreamEvent{Type: StreamEventTextDelta, Text: delta.Content}) {
					return
				}
			}

			// Tool call delta
			for _, tc := range delta.ToolCalls {
				state, exists := toolCalls[tc.Index]
				if !exists {
					// First chunk for this tool call: contains ID and name
					state = &toolCallState{id: tc.ID, name: tc.Function.Name}
					toolCalls[tc.Index] = state
					if !send(StreamEvent{
						Type:       StreamEventToolCallStart,
						ToolCallID: tc.ID,
						ToolName:   tc.Function.Name,
					}) {
						return
					}
				}

				// Arguments delta (maybe empty on first chunk)
				if tc.Function.Arguments != "" {
					if !send(StreamEvent{
						Type:       StreamEventToolCallDelta,
						ToolCallID: state.id,
						Arguments:  tc.Function.Arguments,
					}) {
						return
					}
				}
			}

			// Finish reason
			if choice.FinishReason != "" {
				// Emit tool_call_end for all tracked tool calls
				for _, state := range toolCalls {
					if !send(StreamEvent{Type: StreamEventToolCallEnd, ToolCallID: state.id}) {
						return
					}
				}
				stopReason := choice.FinishReason // pass through unrecognized values as-is
				switch choice.FinishReason {
				case "stop":
					stopReason = StopReasonEndTurn
				case "tool_calls":
					stopReason = StopReasonToolUse
				case "length":
					stopReason = StopReasonMaxTokens
				case "content_filter":
					stopReason = StopReasonContentFilter
				}
				if !send(StreamEvent{Type: StreamEventDone, StopReason: stopReason}) {
					return
				}
			}
			if err := stream.Err(); err != nil {
				send(StreamEvent{Type: StreamEventError, Error: err.Error()})
			}
		}
	}()

	return ch, nil
}

func toOpenAIMessages(msgs []Message) []openai.ChatCompletionMessageParamUnion {
	var out []openai.ChatCompletionMessageParamUnion
	for _, m := range msgs {
		switch {
		case m.Role == "system":
			out = append(out, openai.SystemMessage(m.Content))
		case m.Role == "user":
			out = append(out, openai.UserMessage(m.Content))
		case m.Role == "tool":
			out = append(out, openai.ToolMessage(m.Content, m.ToolCallID))
		case m.Role == "assistant" && len(m.ToolCalls) > 0:
			msg := openai.ChatCompletionMessageParamOfAssistant(m.Content)
			var tcs []openai.ChatCompletionMessageToolCallParam
			for _, tc := range m.ToolCalls {
				tcs = append(tcs, openai.ChatCompletionMessageToolCallParam{
					ID: tc.ID,
					Function: openai.ChatCompletionMessageToolCallFunctionParam{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				})
			}
			msg.OfAssistant.ToolCalls = tcs
			out = append(out, msg)
		case m.Role == "assistant":
			out = append(out, openai.AssistantMessage(m.Content))
		}
	}
	return out
}

func toOpenAITools(tools []Tool) []openai.ChatCompletionToolParam {
	var out []openai.ChatCompletionToolParam
	for _, t := range tools {
		out = append(out, openai.ChatCompletionToolParam{
			Function: shared.FunctionDefinitionParam{
				Name:        t.Function.Name,
				Description: openai.String(t.Function.Description),
				Parameters:  shared.FunctionParameters(t.Function.Parameters),
			},
		})
	}
	return out
}
