package chat

// WSEvent is a full state snapshot pushed to WebSocket clients on every update.
// Each push contains the complete current state of the agent's reply, so the
// client can render by simply replacing its local state. No delta accumulation
// needed, which means reconnecting clients recover immediately.
type WSEvent struct {
	SessionID string       `json:"session_id"`
	AgentID   string       `json:"agent_id"`
	Status    string       `json:"status"` // streaming, tool_calling, done, error
	Text      string       `json:"text"`
	ToolCalls []WSToolCall `json:"tool_calls"`
	Error     string       `json:"error,omitempty"`
}

type WSToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Status    string `json:"status"` // running, completed, failed
	Result    string `json:"result,omitempty"`
}

const (
	WSStatusStreaming   = "streaming"
	WSStatusToolCalling = "tool_calling"
	WSStatusDone        = "done"
	WSStatusError       = "error"
)

const (
	WSToolStatusRunning   = "running"
	WSToolStatusCompleted = "completed"
	WSToolStatusFailed    = "failed"
)
