// Package wsbus abstracts WebSocket message delivery and connection
// tracking so the rest of the codebase does not care whether it is
// running behind AWS API Gateway WebSocket (where the gateway owns the
// socket and we push via PostToConnection), or holding the socket
// in-process (Cloud Run, Container Apps, K8s, plain docker, tests).
//
// Bus is the "send a payload to one connection" seam.
// Registry is the "who is connected as what user" seam.
//
// Application code that needs to push to every active connection of a
// chat session loops chat.Service.ListSessionMembers, then Registry.GetByUser,
// then Bus.Send per connection ID. The loop is plain Go; only the leaf
// operations cross the abstraction.
package wsbus

import (
	"context"
	"errors"
)

// ErrConnGone signals that a connection no longer exists on the remote
// (e.g. API Gateway returned 410 Gone). Callers typically respond by
// unregistering the stale connection ID.
var ErrConnGone = errors.New("wsbus: connection gone")

// Bus sends a payload to exactly one WebSocket connection.
//
// Implementations may be remote (push via a managed gateway HTTP API)
// or local (write to a *websocket.Conn held in-process). The contract
// is the same: best-effort delivery for one connID.
type Bus interface {
	Send(ctx context.Context, connID string, payload []byte) error
}

// Registry tracks live connections. Each connection is keyed by the
// gateway- or server-assigned connID and tagged with the userID and
// workspaceID it was opened for.
//
// Workspace membership and session membership live elsewhere
// (system.Service, chat.Service) since they participate in non-WS authz
// too. Registry is only the connection table.
type Registry interface {
	Register(ctx context.Context, connID, userID, workspaceID string) error
	Unregister(ctx context.Context, connID string) error
	GetByUser(ctx context.Context, userID string) ([]string, error)
}
