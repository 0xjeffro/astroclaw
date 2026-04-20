package chat

import (
	"errors"
	"iclaw/pkg/provider"
	"time"
)

var (
	ErrNotFound = errors.New("not found") // session or message doesn't exist
)

type User struct {
	ID        string
	Email     string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Session struct {
	ID              string
	UserID          string
	Title           string
	Model           string
	SystemPrompt    string
	ContextWindow   int
	ContextMessages []Message // Agent's compressed working context (JSONB in PostgreSQL)
	ContextSummary  string    // Agent's conversation summary
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time // nil = active, non-nil = soft deleted
}

type SessionMember struct {
	SessionID string
	UserID    string
	Role      string // "owner" | "guest"
	JoinedAt  time.Time
}

type Message struct {
	ID             string
	SessionID      string
	Role           string              // "system" | "user" | "assistant" | "tool"
	Content        string              // message text, or tool execution result when Role="tool"
	ToolCalls      []provider.ToolCall // only for Role="assistant": tools the model wants to call
	ToolCallID     string              // only for Role="tool": which ToolCall this result responds to
	SequenceNumber int                 // ordering within a session, monotonically increasing
	ForwardedFrom  string              // source message ID if forwarded, empty if original
	ReplyTo        string              // quoted message ID if replying to a specific message, empty otherwise
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
