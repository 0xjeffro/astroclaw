package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"iclaw/pkg/app/chat/db"
	"iclaw/pkg/provider"
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
	ContextMessages []Message // Agent's compressed working context (TEXT in PostgreSQL)
	ContextSummary  string    // Agent's conversation summary
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time // nil = active, non-nil = soft deleted
}

// SessionFromDB constructs a chat.Session from a db.AppChatSession.
func SessionFromDB(s db.AppChatSession) (*Session, error) {
	var contextMsgs []Message
	if len(s.ContextMessages) > 0 {
		if err := json.Unmarshal([]byte(s.ContextMessages), &contextMsgs); err != nil {
			return nil, fmt.Errorf("unmarshal context messages: %w", err)
		}
	}
	return &Session{
		ID:              s.ID,
		UserID:          s.UserID,
		Title:           s.Title,
		Model:           s.Model,
		SystemPrompt:    s.SystemPrompt,
		ContextWindow:   int(s.ContextWindow),
		ContextMessages: contextMsgs,
		ContextSummary:  s.ContextSummary,
		CreatedAt:       s.CreatedAt,
		UpdatedAt:       s.UpdatedAt,
		DeletedAt:       s.DeletedAt,
	}, nil
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
