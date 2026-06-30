package aws

import (
	"context"

	"astroclaw/pkg/app/system"
)

// Registry stores connections in the app_system_connections table.
// AWS needs durable storage because Lambdas are stateless, so a DB row
// is the only place to remember "this conn ID belongs to that user".
type Registry struct {
	svc *system.Service
}

// NewRegistry adapts an existing *system.Service to the wsbus.Registry
// interface. The service keeps owning the underlying SQL queries; this
// type only exposes the WebSocket-shaped subset.
func NewRegistry(svc *system.Service) *Registry {
	return &Registry{svc: svc}
}

func (r *Registry) Register(ctx context.Context, connID, userID, workspaceID string) error {
	return r.svc.CreateWSConnectRecord(ctx, connID, userID, workspaceID)
}

func (r *Registry) Unregister(ctx context.Context, connID string) error {
	return r.svc.DeleteWSConnectRecord(ctx, connID)
}

func (r *Registry) GetByUser(ctx context.Context, userID string) ([]string, error) {
	conns, err := r.svc.GetConnectionsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(conns))
	for i, c := range conns {
		ids[i] = c.ConnectionID
	}
	return ids, nil
}
