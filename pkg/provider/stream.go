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
