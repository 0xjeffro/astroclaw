package agents

import (
	"context"
	"fmt"

	"iclaw/pkg/app/agents/db"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	queries *db.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{queries: db.New(pool)}
}

func (svc *Service) CreateAgent(ctx context.Context, name, soul, model string) (*Agent, error) {
	a, err := svc.queries.CreateAgent(ctx, db.CreateAgentParams{
		Name:  name,
		Soul:  soul,
		Model: model,
	})
	if err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}
	return AgentFromDB(a), nil
}

func (svc *Service) GetAgent(ctx context.Context, id string) (*Agent, error) {
	a, err := svc.queries.GetAgent(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("agent %q: %w", id, ErrNotFound)
	}
	return AgentFromDB(a), nil
}

func (svc *Service) ListAgents(ctx context.Context) ([]*Agent, error) {
	rows, err := svc.queries.ListAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	agents := make([]*Agent, len(rows))
	for i, a := range rows {
		agents[i] = AgentFromDB(a)
	}
	return agents, nil
}
