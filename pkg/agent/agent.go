package agent

import (
	"context"
	"fmt"
	"iclaw/pkg/provider"
	"iclaw/pkg/tool"
)

type Agent struct {
	provider provider.Provider
	system   string
	history  []provider.Message
	tools    *tool.Registry

	contextWindow int    // model's context window size in tokens, 0 = no limit
	summary       string // LLM-generated summary of older conversation turns

	OnApproval func(toolName string, args string) bool // return false to deny tool execution, nil = auto-approve

	OnToolCall   func(id string, toolName string, args string)   // called before tool execution, nil = silent
	OnToolResult func(id string, toolName string, result string) // called after tool execution, nil = silent
	OnTextDelta  func(text string)
}

func New(p provider.Provider, system string, tools *tool.Registry, contextWindow int) *Agent {
	return &Agent{provider: p, system: system, tools: tools, contextWindow: contextWindow}
}

// NewFromContext creates an Agent with an existing conversation context,
// used when restoring a session. contextMessages is the Agent's compressed
// working history, contextSummary is the LLM-generated summary of older turns.
func NewFromContext(p provider.Provider, system string, tools *tool.Registry,
	contextWindow int, contextMessages []provider.Message, contextSummary string) *Agent {
	a := New(p, system, tools, contextWindow)
	a.history = contextMessages
	a.summary = contextSummary
	return a
}

func (a *Agent) ClearHistory() {
	a.history = []provider.Message{}
}

func (a *Agent) History() []provider.Message {
	return a.history
}

func (a *Agent) Summary() string {
	return a.summary
}

func (a *Agent) Reply(ctx context.Context, userText string) (string, error) {

	historyLen := len(a.history)
	// Add the user message to the history.
	a.history = append(a.history, provider.Message{Role: "user", Content: userText})
	providerTools := toProviderTools(a.tools)

	const maxSteps = 10
	for step := 0; step < maxSteps; step++ {

		// Check Context Budget and summarize old turns if needed.
		if a.isOverBudget() {
			if err := a.summarizeOldTurns(ctx); err != nil {
				// If summarization fails, execute force compress.
				a.forceCompress()
			}

			// If summarization success but still exceed the budget, execute force compress.
			if a.isOverBudget() {
				a.forceCompress()
			}
		}

		// Construct the message list for this call.
		var msgs []provider.Message
		if a.system != "" || a.summary != "" { // If the system prompt or summary is not empty, add them to the history.
			systemContent := a.system
			if a.summary != "" {
				if systemContent != "" {
					systemContent += "\n\n"
				}
				systemContent += "Previous conversation summary:\n" + a.summary
			}
			msgs = append(msgs, provider.Message{Role: "system", Content: systemContent})
		}
		msgs = append(msgs, a.history...) // Add the entire history to the message list (including the userText this time).

		// Call the provider with the full message list and tools.
		ch, err := a.provider.ChatStream(ctx, msgs, providerTools)

		if err != nil {
			// FIXME: historyLen was captured before context compression (summarize/forceCompress).
			// If compression ran and shortened the history, historyLen points beyond the
			// current slice length, causing a panic. Need to either update historyLen
			// after compression, or use a different rollback strategy.
			a.history = a.history[:historyLen]
			return "", err
		}

		// Consume the stream and assemble the assistant's reply.
		var reply provider.Message
		reply.Role = "assistant"
		var toolCalls []provider.ToolCall
		toolArgs := map[string]string{}

		for event := range ch {
			switch event.Type {
			case provider.StreamEventTextDelta:
				reply.Content += event.Text
				if a.OnTextDelta != nil {
					a.OnTextDelta(event.Text)
				}
			case provider.StreamEventToolCallStart:
				toolCalls = append(toolCalls, provider.ToolCall{
					ID:   event.ToolCallID,
					Type: "function",
					Function: provider.ToolCallFunc{
						Name: event.ToolName,
					},
				})
				toolArgs[event.ToolCallID] = ""
			case provider.StreamEventToolCallDelta:
				toolArgs[event.ToolCallID] += event.Arguments
			case provider.StreamEventToolCallEnd:
				for i := range toolCalls {
					if toolCalls[i].ID == event.ToolCallID {
						toolCalls[i].Function.Arguments = toolArgs[event.ToolCallID]
						break
					}
				}
			case provider.StreamEventError:
				return "", fmt.Errorf("stream error: %s", event.Error)
			}
		}
		reply.ToolCalls = toolCalls
		// Append the assistant's message to the history.
		a.history = append(a.history, reply)

		// TODO: the agent loop currently ignores StopReason from StreamEventDone
		// and decides whether to continue based on len(reply.ToolCalls). In the
		// future, StopReason should be used to inform the user about specific
		// conditions (e.g. max_tokens means the reply was truncated,
		// content_filter means the response was blocked).
		if len(reply.ToolCalls) == 0 {
			return reply.Content, nil
		}

		// Otherwise, call the tool and append the result to the history.
		for _, tc := range reply.ToolCalls {
			t, ok := a.tools.Get(tc.Function.Name)

			if !ok {
				// Tool isn't found, return an error as a tool result.
				a.history = append(a.history, provider.Message{
					Role:       "tool",
					ToolCallID: tc.ID,
					Content:    fmt.Sprintf("error: tool %q not found", tc.Function.Name),
				})
				continue // continue to the next tool call
			}

			// Check if this tool needs user approval before execution
			if t.Approval() && a.OnApproval != nil {
				if !a.OnApproval(tc.Function.Name, tc.Function.Arguments) {
					a.history = append(a.history, provider.Message{
						Role:       "tool",
						ToolCallID: tc.ID,
						Content: fmt.Sprintf(
							"User rejected this %s(%s) call. The tool was NOT executed, nothing happened. "+
								"Stop and ask the user how they want to proceed.",
							tc.Function.Name, tc.Function.Arguments,
						),
					})
					continue
				}
			}

			if a.OnToolCall != nil {
				a.OnToolCall(tc.ID, tc.Function.Name, tc.Function.Arguments)
			}

			result, err := safeToolRun(ctx, t, tc.Function.Arguments)
			if err != nil {
				// Tool failed, use the error as a tool result.
				result = fmt.Sprintf("error: %v", err)
			}

			if a.OnToolResult != nil {
				a.OnToolResult(tc.ID, tc.Function.Name, result)
			}

			a.history = append(a.history, provider.Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    result,
			})
		}

	}

	return "", fmt.Errorf("agent: max steps exceeded")
}

// toProviderTools converts tool.Tool (internal, contains Run function) into
// provider.Tool (serializable, sent to LLM). The Run function is stripped
// because it cannot be serialized to JSON. Each Provider implementation
// decides how to serialize the result. For example, OpenAI uses it as-is, Anthropic
// re-maps the fields to its own format inside Chat.
func toProviderTools(reg *tool.Registry) []provider.Tool {
	if reg == nil {
		return nil
	}
	allTools := reg.List()
	out := make([]provider.Tool, 0, len(allTools))
	for _, t := range allTools {
		out = append(out, provider.Tool{
			Type: "function",
			Function: provider.ToolFunction{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Parameters(),
			},
		})
	}
	return out
}

func safeToolRun(ctx context.Context, t tool.Tool, args string) (result string, err error) {
	defer func() {
		if r := recover(); r != nil {
			result = fmt.Sprintf("error: tool panicked: %v", r)
			err = nil
		}
	}()
	return t.Execute(ctx, args)
}
