package store

import (
	"context"
	"fmt"
	"time"
)

type MemoryStore struct {
	sessions map[string]*Session
	messages map[string][]*Message // sessionID → messages
	counter  int                   // for generating unique IDs
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions: make(map[string]*Session),
		messages: make(map[string][]*Message),
	}
}

func (m *MemoryStore) nextID(prefix string) string {
	m.counter++
	return fmt.Sprintf("%s_%d", prefix, m.counter)
}

func (m *MemoryStore) CreateSession(_ context.Context, s *Session) error {
	if s.ID == "" {
		s.ID = m.nextID("s")
	}
	s.CreatedAt = time.Now()
	s.UpdatedAt = s.CreatedAt
	m.sessions[s.ID] = s
	m.messages[s.ID] = nil
	return nil
}

func (m *MemoryStore) GetSession(_ context.Context, id string) (*Session, error) {
	s, ok := m.sessions[id]
	if !ok || s.DeletedAt != nil {
		return nil, fmt.Errorf("session %q: %w", id, ErrNotFound)
	}
	return s, nil
}

func (m *MemoryStore) ListSessions(_ context.Context, userID string) ([]*Session, error) {
	var result []*Session
	for _, s := range m.sessions {
		if s.DeletedAt != nil {
			continue
		}
		if userID == "" || s.UserID == userID {
			result = append(result, s)
		}
	}
	return result, nil
}

func (m *MemoryStore) DeleteSession(_ context.Context, id string) error {
	s, ok := m.sessions[id]
	if !ok || s.DeletedAt != nil {
		return fmt.Errorf("session %q: %w", id, ErrNotFound)
	}
	now := time.Now()
	s.DeletedAt = &now
	return nil
}

func (m *MemoryStore) CreateMessage(_ context.Context, msg *Message) error {
	s, ok := m.sessions[msg.SessionID]
	if !ok || s.DeletedAt != nil {
		return fmt.Errorf("session %q: %w", msg.SessionID, ErrNotFound)
	}
	if msg.ID == "" {
		msg.ID = m.nextID("m")
	}
	msgs := m.messages[msg.SessionID]

	// Auto-assign sequence number.
	if msg.SequenceNumber == 0 {
		if len(msgs) > 0 {
			msg.SequenceNumber = msgs[len(msgs)-1].SequenceNumber + 1
		} else {
			msg.SequenceNumber = 1
		}
	}

	msg.CreatedAt = time.Now()
	msg.UpdatedAt = msg.CreatedAt
	m.messages[msg.SessionID] = append(msgs, msg)
	return nil
}

func (m *MemoryStore) CreateMessages(ctx context.Context, msgs []Message) error {
	for i := range msgs {
		if err := m.CreateMessage(ctx, &msgs[i]); err != nil {
			return err
		}
	}
	return nil
}

func (m *MemoryStore) ListMessages(_ context.Context, sessionID string) ([]*Message, error) {
	s, ok := m.sessions[sessionID]
	if !ok || s.DeletedAt != nil {
		return nil, fmt.Errorf("session %q: %w", sessionID, ErrNotFound)
	}
	return m.messages[sessionID], nil
}

func (m *MemoryStore) UpdateSession(_ context.Context, s *Session) error {
	existing, ok := m.sessions[s.ID]
	if !ok || existing.DeletedAt != nil {
		return fmt.Errorf("session %q: %w", s.ID, ErrNotFound)
	}
	s.UpdatedAt = time.Now()
	m.sessions[s.ID] = s
	return nil
}
