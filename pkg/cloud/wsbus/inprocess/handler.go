package inprocess

import (
	"log"
	"net/http"

	"astroclaw/pkg/cloud/wsbus"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// AuthFunc extracts and verifies (userID, workspaceID) from an incoming
// WebSocket upgrade request. A non-nil error rejects the connection.
// Implementations typically parse a JWT from a query param or header.
type AuthFunc func(r *http.Request) (userID, workspaceID string, err error)

// upgrader is shared across all Handlers. CheckOrigin is permissive
// because the WS endpoint is protected by application-level auth via
// AuthFunc; production callers who want stricter CORS can wrap the
// returned handler.
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Handler returns an http.HandlerFunc that upgrades the request to a
// WebSocket, authenticates it, registers the connection on both the
// Bus (for Bus.Send lookup) and the shared Registry (for cross-instance
// state and observability), and blocks in a read loop until the client
// disconnects.
//
// Incoming messages are drained but not processed: the WebSocket in
// this app is a one-way push channel from server to client.
func Handler(bus *Bus, registry wsbus.Registry, auth AuthFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, workspaceID, err := auth(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			// Upgrade already wrote the error response.
			log.Printf("inprocess: ws upgrade failed: %v", err)
			return
		}
		defer func() { _ = ws.Close() }()

		ctx := r.Context()
		connID := uuid.NewString()

		bus.track(connID, ws)
		if err := registry.Register(ctx, connID, userID, workspaceID); err != nil {
			log.Printf("inprocess: registry register %s: %v", connID, err)
			bus.untrack(connID)
			return
		}
		defer func() {
			if err := registry.Unregister(ctx, connID); err != nil {
				log.Printf("inprocess: registry unregister %s: %v", connID, err)
			}
			bus.untrack(connID)
		}()

		// Reading detects disconnect so we can clean up. gorilla handles
		// ping/pong automatically as long as a read is in flight.
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}
}
