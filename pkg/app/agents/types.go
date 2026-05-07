package agents

import (
	"errors"
	"time"

	"astroclaw/pkg/app/agents/db"
)

var (
	ErrNotFound = errors.New("agent not found")
)

type Agent struct {
	ID        string
	Name      string
	Soul      string
	Model     string
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

func AgentFromDB(a db.AppAgentsProfile) *Agent {
	return &Agent{
		ID:        a.ID,
		Name:      a.Name,
		Soul:      a.Soul,
		Model:     a.Model,
		CreatedAt: a.CreatedAt,
		UpdatedAt: a.UpdatedAt,
		DeletedAt: a.DeletedAt,
	}
}
