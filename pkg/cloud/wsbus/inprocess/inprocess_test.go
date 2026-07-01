package inprocess_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"astroclaw/pkg/app/system"
	"astroclaw/pkg/cloud/wsbus"
	"astroclaw/pkg/cloud/wsbus/inprocess"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// testRegistry is a real wsbus.WsRegistry backed by a Postgres container
// started once in TestMain. All tests in this package share it. Rows are
// isolated by random UUIDs per test, so no explicit cleanup is needed.
var (
	testPool     *pgxpool.Pool
	testRegistry wsbus.Registry
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	migrationFiles, _ := filepath.Glob("../../../../migrations/*.sql")
	sort.Strings(migrationFiles)
	if len(migrationFiles) == 0 {
		panic("no migration files found in ../../../../migrations/*.sql")
	}

	pg, err := postgres.Run(ctx, "postgres:16",
		postgres.WithDatabase("astroclaw_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		postgres.WithInitScripts(migrationFiles...),
		postgres.BasicWaitStrategies(),
		postgres.WithSQLDriver("pgx"),
	)
	if err != nil {
		// Fall back to DATABASE_URL if Docker is unavailable (CI without DIND).
		if cs := os.Getenv("DATABASE_URL"); cs != "" {
			testPool, _ = pgxpool.New(ctx, cs)
			testRegistry = wsbus.NewWsRegistry(system.NewService(testPool))
			code := m.Run()
			testPool.Close()
			os.Exit(code)
		}
		panic("cannot start test database: " + err.Error())
	}

	cs, _ := pg.ConnectionString(ctx, "sslmode=disable")
	testPool, err = pgxpool.New(ctx, cs)
	if err != nil {
		panic("connect to test database: " + err.Error())
	}
	testRegistry = wsbus.NewWsRegistry(system.NewService(testPool))

	code := m.Run()

	testPool.Close()
	if pg != nil {
		_ = pg.Terminate(ctx)
	}
	os.Exit(code)
}

// TestPushRoundup proves the full round trip: client upgrades against
// the Handler, Registry.GetByUser (real DB row) surfaces the connID,
// Bus.Send writes to the socket, client reads the payload.
func TestPushRoundTrip(t *testing.T) {
	bus := inprocess.NewBus()
	userID := uuid.NewString()
	workspaceID := uuid.NewString()

	authFn := func(r *http.Request) (string, string, error) {
		return userID, workspaceID, nil
	}

	srv := httptest.NewServer(inprocess.Handler(bus, testRegistry, authFn))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()

	ctx := context.Background()
	var connID string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ids, _ := testRegistry.GetByUser(ctx, userID)
		if len(ids) == 1 {
			connID = ids[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if connID == "" {
		t.Fatal("connection never appeared in registry")
	}

	if err := bus.Send(ctx, connID, []byte("hello")); err != nil {
		t.Fatalf("send: %v", err)
	}

	if err := c.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	_, msg, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(msg) != "hello" {
		t.Fatalf("got %q, want hello", string(msg))
	}
}

// TestSendMissingConn checks the ErrConnGone signal that the reply
// pipeline uses to unregister stale connections. This one needs no
// Registry — Bus alone is enough.
func TestSendMissingConn(t *testing.T) {
	bus := inprocess.NewBus()
	err := bus.Send(context.Background(), "does-not-exist", []byte("x"))
	if err == nil {
		t.Fatal("expected error for unknown connID, got nil")
	}
}

// TestDisconnectCleansRegistry verifies the handler unregisters the
// conn from the (real DB) Registry when the client closes.
func TestDisconnectCleansRegistry(t *testing.T) {
	bus := inprocess.NewBus()
	userID := uuid.NewString()
	workspaceID := uuid.NewString()

	authFn := func(*http.Request) (string, string, error) {
		return userID, workspaceID, nil
	}
	srv := httptest.NewServer(inprocess.Handler(bus, testRegistry, authFn))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	ctx := context.Background()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ids, _ := testRegistry.GetByUser(ctx, userID); len(ids) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	_ = c.Close()

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ids, _ := testRegistry.GetByUser(ctx, userID); len(ids) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("registry never cleared after client disconnect")
}
