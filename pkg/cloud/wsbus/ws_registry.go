package wsbus

import (
	"context"

	"astroclaw/pkg/app/system"
)

// WsRegistry stores connections in the app_system_connections table.
// Shared across deployment models: AWS uses it because Lambdas are
// stateless and a DB row is the only durable place to remember
// "this connID belongs to that user"; InProcess uses it so the
// registry has one consistent semantics regardless of where the
// server runs. On single-instance InProcess deploys the caller
// should TRUNCATE this table at startup to drop stale rows left by
// a previous process.
type WsRegistry struct {
	svc *system.Service
}

// NewWsRegistry adapts an existing *system.Service to the Registry
// interface. The service keeps owning the underlying SQL queries; this
// type only exposes the WebSocket-shaped subset.
func NewWsRegistry(svc *system.Service) *WsRegistry {
	return &WsRegistry{svc: svc}
}

func (r *WsRegistry) Register(ctx context.Context, connID, userID, workspaceID string) error {
	return r.svc.CreateWSConnectRecord(ctx, connID, userID, workspaceID)
}

func (r *WsRegistry) Unregister(ctx context.Context, connID string) error {
	return r.svc.DeleteWSConnectRecord(ctx, connID)
}

func (r *WsRegistry) GetByUser(ctx context.Context, userID string) ([]string, error) {
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
