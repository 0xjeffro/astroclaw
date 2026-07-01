// Package inprocess implements wsbus.Bus for container deploys where
// the server holds each *websocket.Conn in memory and pushes messages
// by writing directly to the socket.
//
// The Registry side lives outside this package — callers wire the same
// wsbus.WsRegistry that AWS uses so registration semantics stay
// consistent. Handler (in handler.go) calls Bus.track/untrack for the
// socket map and Registry.Register/Unregister for the DB row.
//
// Suitable for single-instance container deploys (Cloud Run min=max=1,
// docker-compose, Fly.io single VM). Multi-instance fan-out requires a
// pub/sub layer wrapped around the Bus.
package inprocess

import (
	"context"
	"sync"

	"astroclaw/pkg/cloud/wsbus"

	"github.com/gorilla/websocket"
)

// Bus holds live WebSocket connections and implements wsbus.Bus.
// gorilla/websocket requires callers to serialize writes on a single
// connection, so every push takes the wrapped per-conn mutex.
type Bus struct {
	mu    sync.RWMutex
	conns map[string]*conn
}

type conn struct {
	ws      *websocket.Conn
	writeMu sync.Mutex
}

// NewBus returns a Bus with an empty socket map.
func NewBus() *Bus {
	return &Bus{conns: make(map[string]*conn)}
}

// Send writes payload to the connection identified by connID.
// Returns wsbus.ErrConnGone if the connection is not tracked; the
// caller typically responds by unregistering the stale connID.
func (b *Bus) Send(_ context.Context, connID string, payload []byte) error {
	b.mu.RLock()
	c, ok := b.conns[connID]
	b.mu.RUnlock()
	if !ok {
		return wsbus.ErrConnGone
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.ws.WriteMessage(websocket.TextMessage, payload)
}

// track stores a fresh WebSocket connection under connID. Called by
// Handler after a successful upgrade.
func (b *Bus) track(connID string, ws *websocket.Conn) {
	b.mu.Lock()
	b.conns[connID] = &conn{ws: ws}
	b.mu.Unlock()
}

// untrack removes a connection. Idempotent; called on client disconnect
// and when Registry.Register fails after upgrade.
func (b *Bus) untrack(connID string) {
	b.mu.Lock()
	delete(b.conns, connID)
	b.mu.Unlock()
}
