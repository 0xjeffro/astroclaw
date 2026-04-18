package session

// manager.go manages session lifecycle and routes messages to Agent instances.
// Manager is stateless: it creates a fresh Agent for each Reply call by
// loading context from Store, and saves context back after Reply completes.
// This makes it naturally multi-user and cloud-friendly (no in-memory state
// to lose on restart or share across instances).

import (
	"context"
	"fmt"
	"iclaw/pkg/agent"

	"iclaw/pkg/store"
)

type Manager struct {
	store       store.Store
	createAgent func(s *store.Session) *agent.Agent
	configAgent func(a *agent.Agent)
}

func NewManager(
	st store.Store,
	createFn func(s *store.Session) *agent.Agent,
	configFn func(a *agent.Agent),
) *Manager {
	return &Manager{
		store:       st,
		createAgent: createFn,
		configAgent: configFn,
	}
}

// NewSession creates a new session record in Store.
func (m *Manager) NewSession(ctx context.Context, title string) (*store.Session, error) {
	s := &store.Session{Title: title}
	if err := m.store.CreateSession(ctx, s); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return s, nil
}

// ListSessions returns all sessions from the Store.
func (m *Manager) ListSessions(ctx context.Context) ([]*store.Session, error) {
	return m.store.ListSessions(ctx, "")
}

// DeleteSession removes a session from Store.
func (m *Manager) DeleteSession(ctx context.Context, id string) error {
	return m.store.DeleteSession(ctx, id)
}

// Reply loads a session's context from Store, creates a fresh Agent,
// processes the message, persists new messages and updated context back
// to Store, then discards the Agent.
func (m *Manager) Reply(ctx context.Context, sessionID string, text string) (string, error) {
	// Load session and create Agent from its context.
	s, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("session %s not found: %w", sessionID, err)
	}
	a := m.createAgent(s)
	if m.configAgent != nil {
		m.configAgent(a)
	}

	historyBefore := len(a.History())

	reply, err := a.Reply(ctx, text)
	if err != nil {
		return "", err
	}

	// Persistence follows an all-or-nothing approach: if we can't save the
	// messages and context to Store, we return an error and hide the reply.
	// Returning the reply on persistence failure would give the user a false
	// sense that everything is fine, but the next Reply would load stale
	// context from Store and the Agent would "forget" what just happened.
	// Better to fail loudly and let the user retry than silently corrupt
	// the conversation state.

	// Persist new messages to the full chat history (single batch write).
	newMsgs := a.History()[historyBefore:]
	storeMsgs := providerToStoreMessages(sessionID, newMsgs)
	if err := m.store.AppendMessages(ctx, storeMsgs); err != nil {
		return "", fmt.Errorf("failed to persist messages: %w", err)
	}

	// Save Agent's compressed context back to session for next load.
	s.ContextMessages = providerToStoreMessages(sessionID, a.History())
	s.ContextSummary = a.Summary()
	if err := m.store.UpdateSession(ctx, s); err != nil {
		return "", fmt.Errorf("failed to save context: %w", err)
	}

	return reply, nil
}
