package provider

type StreamEvent struct {
	Type       string
	Text       string
	Thinking   string
	ToolCallID string
	ToolName   string
	Arguments  string
	StopReason string
	Error      string
}

const (
	StreamEventTextDelta     = "text_delta"
	StreamEventThinkingDelta = "thinking_delta"
	StreamEventToolCallStart = "tool_call_start"
	StreamEventToolCallDelta = "tool_call_delta"
	StreamEventToolCallEnd   = "tool_call_end"
	StreamEventDone          = "done"
	StreamEventError         = "error"
)

// Unified stop reasons. Each provider maps its native values to these constants:
//
//	iclaw constant          | Anthropic          | OpenAI
//	------------------------+--------------------+-------------------
//	StopReasonEndTurn       | end_turn           | stop
//	StopReasonToolUse       | tool_use           | tool_calls
//	StopReasonMaxTokens     | max_tokens         | length
//	StopReasonContentFilter | (none)             | content_filter
//
// Values not listed above (e.g. Anthropic's stop_sequence, pause_turn, refusal
// or OpenAI's function_call) are passed through as-is in StopReason so they
// can be inspected for debugging.
const (
	StopReasonEndTurn       = "end_turn"
	StopReasonToolUse       = "tool_use"
	StopReasonMaxTokens     = "max_tokens"
	StopReasonContentFilter = "content_filter"
)
