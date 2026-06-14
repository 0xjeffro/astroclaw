package client

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"astroclaw/pkg/api"
	"astroclaw/pkg/app/chat"
	"astroclaw/pkg/app/settings"
	"astroclaw/pkg/app/system"
)

// --- fakes ---
//
// The fakes here mirror the ones in pkg/api/handlers_test.go but live in
// this package so client tests can exercise the real api.NewRouter end
// to end without depending on the api package's internal test fixtures.

type fakeChat struct{}

func (*fakeChat) CreateSessionInWorkspace(_ context.Context, _, _ string, _ []string, _ string) (*chat.Session, error) {
	return nil, nil
}
func (*fakeChat) ListSessionsByWorkspace(_ context.Context, _ string) ([]*chat.Session, error) {
	return nil, nil
}
func (*fakeChat) GetSession(_ context.Context, _ string) (*chat.Session, error) {
	return nil, nil
}
func (*fakeChat) SoftDeleteSession(_ context.Context, _ string) error { return nil }

type fakeSettings struct{}

func (*fakeSettings) GetSystemSetting(_ context.Context, _ string) (*settings.SystemSetting, error) {
	return nil, nil
}
func (*fakeSettings) GetWorkspaceSetting(_ context.Context, _, _ string) (*settings.WorkspaceSetting, error) {
	return nil, nil
}
func (*fakeSettings) UpsertWorkspaceSetting(_ context.Context, _, _, _ string) error { return nil }

type fakeSystem struct {
	verifyUser *system.User
	verifyErr  error
	admin      *system.User
	adminErr   error
	wsList     []*system.Workspace
	listErr    error
	meUser     *system.User
	meErr      error
}

func (f *fakeSystem) GetUser(_ context.Context, _ string) (*system.User, error) {
	return f.meUser, f.meErr
}

func (f *fakeSystem) VerifyUserPassword(_ context.Context, _, _ string) (*system.User, error) {
	return f.verifyUser, f.verifyErr
}
func (f *fakeSystem) GetAdmin(_ context.Context) (*system.User, error) {
	return f.admin, f.adminErr
}
func (f *fakeSystem) ListWorkspacesForUser(_ context.Context, _ string) ([]*system.Workspace, error) {
	return f.wsList, f.listErr
}

func stubSecret(_ context.Context) ([]byte, error) {
	return []byte("0123456789abcdef0123456789abcdef"), nil
}

// newTestServer spins up a real api.NewRouter against fake services,
// wraps it in an httptest.Server, and returns a Client pointed at it.
// The server is torn down when the test ends.
func newTestServer(t *testing.T, sys *fakeSystem) (*httptest.Server, *Client) {
	t.Helper()
	router := api.NewRouter(&fakeChat{}, &fakeSettings{}, sys, stubSecret)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server, New(server.URL)
}

// --- tests ---

// Happy path: Login returns a token, stores it internally, and decodes
// the user record correctly.
func TestLoginValid(t *testing.T) {
	_, c := newTestServer(t, &fakeSystem{
		verifyUser: &system.User{ID: "u-1", Email: "admin@x.com", Role: string(system.RoleAdmin)},
	})

	resp, err := c.Login(context.Background(), "admin@x.com", "hunter2")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if resp.Token == "" {
		t.Error("response token empty")
	}
	if c.Token() != resp.Token {
		t.Error("client did not store token internally after login")
	}
	if resp.User == nil || resp.User.ID != "u-1" {
		t.Errorf("user in response: got %+v", resp.User)
	}
}

// Wrong credentials produce ErrInvalidCredentials, not ErrUnauthorized,
// so the CLI can distinguish "wrong password" from "stale token".
func TestLoginInvalidCredentials(t *testing.T) {
	_, c := newTestServer(t, &fakeSystem{verifyErr: system.ErrInvalidCredentials})

	_, err := c.Login(context.Background(), "wrong@x.com", "nope")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
	if c.Token() != "" {
		t.Error("client should not store token on login failure")
	}
}

// After a successful login the stored token is attached to subsequent
// calls; the protected endpoint returns 200 with the decoded user.
func TestGetAdminAuthenticated(t *testing.T) {
	_, c := newTestServer(t, &fakeSystem{
		verifyUser: &system.User{ID: "u-1", Role: string(system.RoleAdmin)},
		admin:      &system.User{ID: "u-admin", Email: "admin@x.com"},
	})

	if _, err := c.Login(context.Background(), "any@x.com", "pw"); err != nil {
		t.Fatalf("login: %v", err)
	}

	u, err := c.GetAdmin(context.Background())
	if err != nil {
		t.Fatalf("get admin: %v", err)
	}
	if u.ID != "u-admin" {
		t.Errorf("admin: got %+v", u)
	}
}

// Without a token a protected call surfaces as ErrUnauthorized so the
// CLI can prompt for re-login.
func TestGetAdminWithoutToken(t *testing.T) {
	_, c := newTestServer(t, &fakeSystem{})

	_, err := c.GetAdmin(context.Background())
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

// ListUserWorkspaces decodes the JSON array correctly and preserves order.
func TestListUserWorkspaces(t *testing.T) {
	_, c := newTestServer(t, &fakeSystem{
		verifyUser: &system.User{ID: "u-1", Role: string(system.RoleUser)},
		wsList: []*system.Workspace{
			{ID: "ws-1", Name: "alpha"},
			{ID: "ws-2", Name: "beta"},
		},
	})

	if _, err := c.Login(context.Background(), "u@x.com", "pw"); err != nil {
		t.Fatalf("login: %v", err)
	}

	ws, err := c.ListUserWorkspaces(context.Background(), "u-1")
	if err != nil {
		t.Fatalf("list workspaces: %v", err)
	}
	if len(ws) != 2 || ws[0].ID != "ws-1" || ws[1].Name != "beta" {
		t.Errorf("workspaces: got %+v", ws)
	}
}
