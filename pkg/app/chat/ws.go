package chat

// WSEvent is the JSON payload pushed to WebSocket clients during streaming.
// The Type field reuses provider.StreamEvent constants (text_delta, tool_call_start, etc.).
// TODO: tentative struct, subject to iteration as streaming requirements become clearer.
type WSEvent struct {
	Type       string `json:"type"`
	SessionID  string `json:"session_id"`
	AgentID    string `json:"agent_id"`
	Text       string `json:"text,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	Arguments  string `json:"arguments,omitempty"`
	Error      string `json:"error,omitempty"`
}
