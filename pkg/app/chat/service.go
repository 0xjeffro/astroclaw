package chat

import (
	"context"
	"encoding/json"
	"fmt"

	"iclaw/pkg/agent"
	"iclaw/pkg/app/chat/db"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Service manages the session lifecycle and routes messages to Agent instances.
// It's stateless: creates a fresh Agent for each Reply call by loading
// context from the database and saves context back after Reply completes.
type Service struct {
	pool        *pgxpool.Pool
	queries     *db.Queries
	createAgent func(s *Session) *agent.Agent
	configAgent func(a *agent.Agent)
}

func NewService(
	pool *pgxpool.Pool,
	createFn func(s *Session) *agent.Agent,
	configFn func(a *agent.Agent),
) *Service {
	return &Service{
		pool:        pool,
		queries:     db.New(pool),
		createAgent: createFn,
		configAgent: configFn,
	}
}

func (svc *Service) NewSession(ctx context.Context, userID string, title string) (*Session, error) {
	s, err := svc.queries.CreateSession(ctx, db.CreateSessionParams{
		Title:           title,
		UserID:          userID,
		ContextMessages: "[]",
	})
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	session, err := SessionFromDB(s)
	if err != nil {
		return nil, fmt.Errorf("parse session: %w", err)
	}
	return session, nil
}

func (svc *Service) ListSessions(ctx context.Context) ([]*Session, error) {
	dbSessions, err := svc.queries.ListSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	sessions := make([]*Session, len(dbSessions))
	for i, s := range dbSessions {
		sessions[i], err = SessionFromDB(s)
		if err != nil {
			return nil, fmt.Errorf("parse session: %w", err)
		}
	}
	return sessions, nil
}

func (svc *Service) GetSession(ctx context.Context, id string) (*Session, error) {
	s, err := svc.queries.GetSession(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("session %q: %w", id, ErrNotFound)
	}
	session, err := SessionFromDB(s)
	if err != nil {
		return nil, fmt.Errorf("parse session: %w", err)
	}
	return session, nil
}

func (svc *Service) SoftDeleteSession(ctx context.Context, id string) error {
	return svc.queries.SoftDeleteSession(ctx, id)
}

// Reply loads a session's context, creates a fresh Agent, processes the
// message, persists new messages and updated context, then discards the Agent.
// Persistence uses a database transaction: messages + context update either
// both succeed or both roll back, preventing inconsistent state.
func (svc *Service) Reply(ctx context.Context, sessionID string, text string) (string, error) {
	s, err := svc.GetSession(ctx, sessionID)
	if err != nil {
		return "", err
	}
	a := svc.createAgent(s)
	if svc.configAgent != nil {
		svc.configAgent(a)
	}

	historyBefore := len(a.History())

	reply, err := a.Reply(ctx, text)
	if err != nil {
		return "", err
	}

	// Begin transaction: messages + context must be saved atomically.
	// If either fails, everything rolls back and the reply is hidden.
	tx, err := svc.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := svc.queries.WithTx(tx)

	// Persist new messages to the full chat history.
	newMsgs := a.History()[historyBefore:]
	for i, msg := range newMsgs {
		_, err := qtx.CreateMessage(ctx, db.CreateMessageParams{
			SessionID:      sessionID,
			Role:           msg.Role,
			Content:        msg.Content,
			ToolCalls:      pgtype.Text{String: string(mustJSON(msg.ToolCalls)), Valid: true},
			ToolCallID:     msg.ToolCallID,
			SequenceNumber: int32(historyBefore + i + 1),
		})
		if err != nil {
			return "", fmt.Errorf("failed to persist message: %w", err)
		}
	}

	// Save Agent's compressed context back to session for next load.
	contextMsgs := providerToChatMessages(sessionID, a.History())
	contextJSON, _ := json.Marshal(contextMsgs)
	err = qtx.UpdateSession(ctx, db.UpdateSessionParams{
		Title:           s.Title,
		Model:           s.Model,
		SystemPrompt:    s.SystemPrompt,
		ContextWindow:   int32(s.ContextWindow),
		ContextMessages: string(contextJSON),
		ContextSummary:  a.Summary(),
		ID:              sessionID,
	})
	if err != nil {
		return "", fmt.Errorf("failed to save context: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit transaction: %w", err)
	}

	return reply, nil
}
