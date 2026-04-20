package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"iclaw/pkg/provider"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(connString string) (*PostgresStore, error) {
	db, err := sql.Open("pgx", connString)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &PostgresStore{db: db}, nil
}

func (p *PostgresStore) Close() error {
	return p.db.Close()
}

func (p *PostgresStore) CreateSession(ctx context.Context, s *Session) error {
	err := p.db.QueryRowContext(ctx,
		`INSERT INTO sessions (title, user_id, model, system_prompt, context_window, context_messages, context_summary)
                 VALUES ($1, $2, $3, $4, $5, $6, $7)
                 RETURNING id, created_at, updated_at`,
		s.Title, s.UserID, s.Model, s.SystemPrompt, s.ContextWindow,
		mustJSON(s.ContextMessages), s.ContextSummary,
	).Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func (p *PostgresStore) GetSession(ctx context.Context, id string) (*Session, error) {
	s := &Session{}
	var contextMessagesJSON []byte

	err := p.db.QueryRowContext(ctx,
		`SELECT id, user_id, title, model, system_prompt, context_window,
                        context_messages, context_summary, created_at, updated_at
                 FROM sessions WHERE id = $1 AND deleted_at IS NULL`, id,
	).Scan(
		&s.ID, &s.UserID, &s.Title, &s.Model, &s.SystemPrompt, &s.ContextWindow,
		&contextMessagesJSON, &s.ContextSummary, &s.CreatedAt, &s.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("session %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("query session: %w", err)
	}
	if err := json.Unmarshal(contextMessagesJSON, &s.ContextMessages); err != nil {
		return nil, fmt.Errorf("unmarshal context messages: %w", err)
	}
	return s, nil
}

func (p *PostgresStore) UpdateSession(ctx context.Context, s *Session) error {
	s.UpdatedAt = time.Now()
	result, err := p.db.ExecContext(ctx,
		`UPDATE sessions
                 SET title = $1, model = $2, system_prompt = $3, context_window = $4,
                     context_messages = $5, context_summary = $6, updated_at = $7
                 WHERE id = $8 AND deleted_at IS NULL`,
		s.Title, s.Model, s.SystemPrompt, s.ContextWindow,
		mustJSON(s.ContextMessages), s.ContextSummary, s.UpdatedAt, s.ID,
	)
	if err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("session %q: %w", s.ID, ErrNotFound)
	}
	return nil
}

func (p *PostgresStore) ListSessions(ctx context.Context, userID string) ([]*Session, error) {
	query := `SELECT id, user_id, title, model, system_prompt, context_window,
                         context_summary, created_at, updated_at
                  FROM sessions WHERE deleted_at IS NULL`
	var args []any

	if userID != "" {
		query += " AND user_id = $1"
		args = append(args, userID)
	}
	query += " ORDER BY created_at DESC"

	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var sessions []*Session
	for rows.Next() {
		s := &Session{}
		if err := rows.Scan(
			&s.ID, &s.UserID, &s.Title, &s.Model, &s.SystemPrompt, &s.ContextWindow,
			&s.ContextSummary, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sessions = append(sessions, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}

	return sessions, nil
}

func (p *PostgresStore) DeleteSession(ctx context.Context, id string) error {
	// Soft delete: set deleted_at instead of actually removing rows.
	// Messages are kept intact for potential recovery.
	result, err := p.db.ExecContext(ctx,
		`UPDATE sessions SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("session %q: %w", id, ErrNotFound)
	}
	return nil
}

func (p *PostgresStore) CreateMessage(ctx context.Context, msg *Message) error {
	err := p.db.QueryRowContext(ctx,
		`INSERT INTO messages (session_id, role, content, tool_calls, tool_call_id,
                                       sequence_number, forwarded_from, reply_to)
                 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
                 RETURNING id, created_at, updated_at`,
		msg.SessionID, msg.Role, msg.Content, toolCallsJSON(msg.ToolCalls),
		msg.ToolCallID, msg.SequenceNumber, uuidOrNull(msg.ForwardedFrom), uuidOrNull(msg.ReplyTo),
	).Scan(&msg.ID, &msg.CreatedAt, &msg.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	return nil
}

func (p *PostgresStore) CreateMessages(ctx context.Context, msgs []Message) error {
	if len(msgs) == 0 {
		return nil
	}

	// Build a single INSERT with multiple VALUES rows.
	var sb strings.Builder
	sb.WriteString(`INSERT INTO messages (session_id, role, content, tool_calls, tool_call_id,
                                              sequence_number, forwarded_from, reply_to)
                        VALUES `)

	args := make([]any, 0, len(msgs)*8)
	for i, msg := range msgs {
		if i > 0 {
			sb.WriteString(", ")
		}
		base := i * 8
		_, _ = fmt.Fprintf(&sb, "($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8)
		args = append(args, msg.SessionID, msg.Role, msg.Content,
			toolCallsJSON(msg.ToolCalls), msg.ToolCallID,
			msg.SequenceNumber, uuidOrNull(msg.ForwardedFrom), uuidOrNull(msg.ReplyTo))
	}

	_, err := p.db.ExecContext(ctx, sb.String(), args...)
	if err != nil {
		return fmt.Errorf("batch insert messages: %w", err)
	}
	return nil
}

func (p *PostgresStore) ListMessages(ctx context.Context, sessionID string) ([]*Message, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, session_id, role, content, tool_calls, tool_call_id,
                        sequence_number, forwarded_from, reply_to, created_at, updated_at
                 FROM messages WHERE session_id = $1 ORDER BY sequence_number`, sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var msgs []*Message
	for rows.Next() {
		m := &Message{}
		var toolCallsJSON []byte
		var forwardedFrom, replyTo sql.NullString
		if err := rows.Scan(
			&m.ID, &m.SessionID, &m.Role, &m.Content, &toolCallsJSON,
			&m.ToolCallID, &m.SequenceNumber, &forwardedFrom, &replyTo,
			&m.CreatedAt, &m.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		m.ForwardedFrom = forwardedFrom.String
		m.ReplyTo = replyTo.String
		if len(toolCallsJSON) > 0 {
			if err := json.Unmarshal(toolCallsJSON, &m.ToolCalls); err != nil {
				return nil, fmt.Errorf("unmarshal tool_calls: %w", err)
			}
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}
	return msgs, nil
}

// helpers

// mustJSON marshals v to JSON, returns "[]" on error.
func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("[]")
	}
	return b
}

// toolCallsJSON marshals ToolCalls to JSON, returns nil if empty (NULL in DB).
func toolCallsJSON(calls []provider.ToolCall) any {
	if len(calls) == 0 {
		return nil
	}
	b, err := json.Marshal(calls)
	if err != nil {
		return nil // NULL in DB, better than corrupted JSON
	}
	return b
}

// uuidOrNull converts an empty string to nil (NULL in DB) for UUID columns.
func uuidOrNull(s string) any {
	if s == "" {
		return nil
	}
	return s
}
