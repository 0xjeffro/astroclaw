package chat

import "context"

type Store interface {
	CreateSession(ctx context.Context, s *Session) error
	GetSession(ctx context.Context, id string) (*Session, error)
	UpdateSession(ctx context.Context, s *Session) error
	ListSessions(ctx context.Context, userID string) ([]*Session, error)
	DeleteSession(ctx context.Context, id string) error

	CreateMessage(ctx context.Context, msg *Message) error
	CreateMessages(ctx context.Context, msgs []Message) error
	ListMessages(ctx context.Context, sessionID string) ([]*Message, error)
}
