package agents

import (
	"context"
	"fmt"

	"astroclaw/pkg/app/agents/db"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, queries: db.New(pool)}
}

// CreateAgent creates an agent profile and attaches it to the given workspace
// in a single transaction.
func (svc *Service) CreateAgent(ctx context.Context, workspaceID, name, soul, model string) (*Agent, error) {
	tx, err := svc.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := svc.queries.WithTx(tx)
	a, err := qtx.CreateAgentProfile(ctx, db.CreateAgentProfileParams{
		Name:  name,
		Soul:  soul,
		Model: model,
	})
	if err != nil {
		return nil, fmt.Errorf("create agent profile: %w", err)
	}
	if err := qtx.AttachAgentToWorkspace(ctx, db.AttachAgentToWorkspaceParams{
		AgentID:     a.ID,
		WorkspaceID: workspaceID,
	}); err != nil {
		return nil, fmt.Errorf("attach agent to workspace: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return AgentFromDB(a), nil
}

func (svc *Service) GetAgentFromWorkspace(ctx context.Context, workspaceID, id string) (*Agent, error) {
	a, err := svc.queries.GetAgentFromWorkspace(ctx, db.GetAgentFromWorkspaceParams{
		ID:          id,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return nil, fmt.Errorf("agent %q: %w", id, ErrNotFound)
	}
	return AgentFromDB(a), nil
}

func (svc *Service) ListAgentsByWorkspace(ctx context.Context, workspaceID string) ([]*Agent, error) {
	rows, err := svc.queries.ListAgentsByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	out := make([]*Agent, len(rows))
	for i, a := range rows {
		out[i] = AgentFromDB(a)
	}
	return out, nil
}

func (svc *Service) AttachAgentToWorkspace(ctx context.Context, agentID, workspaceID string) error {
	return svc.queries.AttachAgentToWorkspace(ctx, db.AttachAgentToWorkspaceParams{
		AgentID:     agentID,
		WorkspaceID: workspaceID,
	})
}

func (svc *Service) DetachAgentFromWorkspace(ctx context.Context, agentID, workspaceID string) error {
	return svc.queries.DetachAgentFromWorkspace(ctx, db.DetachAgentFromWorkspaceParams{
		AgentID:     agentID,
		WorkspaceID: workspaceID,
	})
}

func (svc *Service) ListWorkspacesForAgent(ctx context.Context, agentID string) ([]string, error) {
	return svc.queries.ListWorkspacesForAgent(ctx, agentID)
}
