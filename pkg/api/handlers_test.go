// Tests for the chi-based API router. They drive the router via
// httptest.NewRecorder + httptest.NewRequest and use in-memory fake services,
// so no DSQL connection is required.
package api

import (
	"astroclaw/pkg/auth"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"astroclaw/pkg/app/chat"
	"astroclaw/pkg/app/settings"
	"astroclaw/pkg/app/system"
)

// --- fakes -----------------------------------------------------------------

// fakeChat is an in-memory chatService that records the arguments it was
// called with and returns preconfigured results/errors.
type fakeChat struct {
	createCalled struct {
		workspaceID, userID, title string
		agentIDs                   []string
	}
	listWorkspaceID string
	getID, deleteID string
	createSession   *chat.Session
	listResult      []*chat.Session
	getResult       *chat.Session
	createErr       error
	listErr         error
	getErr          error
	softDeleteErr   error
}

func (f *fakeChat) CreateSessionInWorkspace(_ context.Context, workspaceID, userID string, agentIDs []string, title string) (*chat.Session, error) {
	f.createCalled.workspaceID = workspaceID
	f.createCalled.userID = userID
	f.createCalled.agentIDs = agentIDs
	f.createCalled.title = title
	return f.createSession, f.createErr
}

func (f *fakeChat) ListSessionsByWorkspace(_ context.Context, workspaceID string) ([]*chat.Session, error) {
	f.listWorkspaceID = workspaceID
	return f.listResult, f.listErr
}

func (f *fakeChat) GetSession(_ context.Context, id string) (*chat.Session, error) {
	f.getID = id
	return f.getResult, f.getErr
}

func (f *fakeChat) SoftDeleteSession(_ context.Context, id string) error {
	f.deleteID = id
	return f.softDeleteErr
}

// fakeSettings is an in-memory settingsService for handler tests.
type fakeSettings struct {
	getSysName                               string
	getWsWorkspace, getWsName                string
	upsertWorkspace, upsertName, upsertValue string
	sysResult                                *settings.SystemSetting
	wsResult                                 *settings.WorkspaceSetting
	sysErr, wsErr, upsertErr                 error
}

func (f *fakeSettings) GetSystemSetting(_ context.Context, name string) (*settings.SystemSetting, error) {
	f.getSysName = name
	return f.sysResult, f.sysErr
}

func (f *fakeSettings) GetWorkspaceSetting(_ context.Context, workspaceID, name string) (*settings.WorkspaceSetting, error) {
	f.getWsWorkspace = workspaceID
	f.getWsName = name
	return f.wsResult, f.wsErr
}

func (f *fakeSettings) UpsertWorkspaceSetting(_ context.Context, workspaceID, name, value string) error {
	f.upsertWorkspace = workspaceID
	f.upsertName = name
	f.upsertValue = value
	return f.upsertErr
}

// fakeSystem is an in-memory systemService for handler tests.
type fakeSystem struct {
	listUserID string
	admin      *system.User
	wsList     []*system.Workspace
	adminErr   error
	listErr    error

	verifyCalledEmail, verifyCalledPassword string
	verifyUser                              *system.User
	verifyErr                               error
}

func (f *fakeSystem) GetAdmin(_ context.Context) (*system.User, error) {
	return f.admin, f.adminErr
}

func (f *fakeSystem) ListWorkspacesForUser(_ context.Context, userID string) ([]*system.Workspace, error) {
	f.listUserID = userID
	return f.wsList, f.listErr
}

func (f *fakeSystem) VerifyUserPassword(_ context.Context, email, password string) (*system.User, error) {
	f.verifyCalledEmail = email
	f.verifyCalledPassword = password
	return f.verifyUser, f.verifyErr
}

// stubSecret is the test-side getSecret function passed into newRouter.
// It returns the same 32-byte secret used by pkg/auth tests.
func stubSecret(_ context.Context) ([]byte, error) {
	return []byte("0123456789abcdef0123456789abcdef"), nil
}

// --- helpers ---------------------------------------------------------------

// do builds a request (JSON-encoding body when non-nil), serves it through
// the handler, and returns the recorded response.
func do(t *testing.T, h http.Handler, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != nil {
		buf, _ := json.Marshal(body)
		r = httptest.NewRequest(method, target, bytes.NewReader(buf))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, target, nil)
	}

	// Attach a valid bearer token so requests pass the auth middleware
	// mounted on protected routes. Tests that want to verify the unauth
	// case build their own request and skip this helper.
	secret, _ := stubSecret(context.Background())
	now := time.Now()
	tok, _ := auth.Sign(secret, auth.Claims{
		UserID:     "u-test",
		SystemRole: system.RoleAdmin,
		Issued:     now,
		Expiry:     now.Add(time.Hour),
	})
	r.Header.Set("Authorization", "Bearer "+tok)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// --- tests -----------------------------------------------------------------

func TestCreateSessionInWorkspace(t *testing.T) {
	chatSvc := &fakeChat{createSession: &chat.Session{ID: "s1", WorkspaceID: "ws1"}}
	r := NewRouter(chatSvc, &fakeSettings{}, &fakeSystem{}, stubSecret)

	body := map[string]any{
		"user_id":   "u1",
		"agent_ids": []string{"a1", "a2"},
		"title":     "hello",
	}
	w := do(t, r, http.MethodPost, "/workspaces/ws1/sessions", body)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	if chatSvc.createCalled.workspaceID != "ws1" || chatSvc.createCalled.userID != "u1" ||
		chatSvc.createCalled.title != "hello" || len(chatSvc.createCalled.agentIDs) != 2 {
		t.Fatalf("unexpected service args: %+v", chatSvc.createCalled)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected json content-type, got %q", ct)
	}
}

func TestListSessions(t *testing.T) {
	chatSvc := &fakeChat{listResult: []*chat.Session{{ID: "s1"}, {ID: "s2"}}}
	r := NewRouter(chatSvc, &fakeSettings{}, &fakeSystem{}, stubSecret)

	w := do(t, r, http.MethodGet, "/workspaces/ws1/sessions", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if chatSvc.listWorkspaceID != "ws1" {
		t.Errorf("expected workspaceID=ws1, got %q", chatSvc.listWorkspaceID)
	}
	var got []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(got))
	}
}

func TestGetSession(t *testing.T) {
	chatSvc := &fakeChat{getResult: &chat.Session{ID: "abc"}}
	r := NewRouter(chatSvc, &fakeSettings{}, &fakeSystem{}, stubSecret)

	w := do(t, r, http.MethodGet, "/workspaces/ws1/sessions/abc", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if chatSvc.getID != "abc" {
		t.Errorf("expected sessionID=abc, got %q", chatSvc.getID)
	}
}

func TestGetSessionNotFound(t *testing.T) {
	chatSvc := &fakeChat{getErr: chat.ErrNotFound}
	r := NewRouter(chatSvc, &fakeSettings{}, &fakeSystem{}, stubSecret)

	w := do(t, r, http.MethodGet, "/workspaces/ws1/sessions/missing", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestGetSessionInternalError(t *testing.T) {
	chatSvc := &fakeChat{getErr: errors.New("boom")}
	r := NewRouter(chatSvc, &fakeSettings{}, &fakeSystem{}, stubSecret)

	// statusForError maps only known sentinel errors (chat.ErrNotFound etc.)
	// to 404; everything else falls through to the call-site default of 500.
	w := do(t, r, http.MethodGet, "/workspaces/ws1/sessions/x", nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 fallback, got %d", w.Code)
	}
}

func TestDeleteSession(t *testing.T) {
	chatSvc := &fakeChat{}
	r := NewRouter(chatSvc, &fakeSettings{}, &fakeSystem{}, stubSecret)

	w := do(t, r, http.MethodDelete, "/workspaces/ws1/sessions/xyz", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if chatSvc.deleteID != "xyz" {
		t.Errorf("expected deleteID=xyz, got %q", chatSvc.deleteID)
	}
}

func TestGetSystemSetting(t *testing.T) {
	settingsSvc := &fakeSettings{sysResult: &settings.SystemSetting{Name: "k", Value: "v"}}
	r := NewRouter(&fakeChat{}, settingsSvc, &fakeSystem{}, stubSecret)

	w := do(t, r, http.MethodGet, "/settings/k", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if settingsSvc.getSysName != "k" {
		t.Errorf("expected name=k, got %q", settingsSvc.getSysName)
	}
}

func TestGetWorkspaceSetting(t *testing.T) {
	settingsSvc := &fakeSettings{wsResult: &settings.WorkspaceSetting{WorkspaceID: "ws1", Name: "k", Value: "v"}}
	r := NewRouter(&fakeChat{}, settingsSvc, &fakeSystem{}, stubSecret)

	w := do(t, r, http.MethodGet, "/workspaces/ws1/settings/k", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if settingsSvc.getWsWorkspace != "ws1" || settingsSvc.getWsName != "k" {
		t.Errorf("unexpected args: ws=%q name=%q", settingsSvc.getWsWorkspace, settingsSvc.getWsName)
	}
}

func TestUpsertWorkspaceSetting(t *testing.T) {
	settingsSvc := &fakeSettings{}
	r := NewRouter(&fakeChat{}, settingsSvc, &fakeSystem{}, stubSecret)

	w := do(t, r, http.MethodPut, "/workspaces/ws1/settings/k", map[string]string{"value": "newval"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if settingsSvc.upsertWorkspace != "ws1" || settingsSvc.upsertName != "k" || settingsSvc.upsertValue != "newval" {
		t.Errorf("unexpected upsert args: %+v", settingsSvc)
	}
}

func TestGetAdmin(t *testing.T) {
	systemSvc := &fakeSystem{admin: &system.User{ID: "u-admin"}}
	r := NewRouter(&fakeChat{}, &fakeSettings{}, systemSvc, stubSecret)

	w := do(t, r, http.MethodGet, "/users/admin", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestListUserWorkspaces(t *testing.T) {
	systemSvc := &fakeSystem{wsList: []*system.Workspace{{ID: "ws1"}}}
	r := NewRouter(&fakeChat{}, &fakeSettings{}, systemSvc, stubSecret)

	w := do(t, r, http.MethodGet, "/users/u1/workspaces", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if systemSvc.listUserID != "u1" {
		t.Errorf("expected userID=u1, got %q", systemSvc.listUserID)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	r := NewRouter(&fakeChat{}, &fakeSettings{}, &fakeSystem{}, stubSecret)

	// PATCH on /users/admin is not registered; chi returns 405.
	w := do(t, r, http.MethodPatch, "/users/admin", nil)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

// Protected routes require a bearer token. Without one, the auth
// middleware should reject the request with 401 before any service
// is called.
func TestProtectedRouteNoToken(t *testing.T) {
	sys := &fakeSystem{admin: &system.User{ID: "u-admin"}}
	r := NewRouter(&fakeChat{}, &fakeSettings{}, sys, stubSecret)

	req := httptest.NewRequest(http.MethodGet, "/users/admin", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

// Expired tokens are rejected even though the signature is valid.
func TestProtectedRouteExpiredToken(t *testing.T) {
	sys := &fakeSystem{admin: &system.User{ID: "u-admin"}}
	r := NewRouter(&fakeChat{}, &fakeSettings{}, sys, stubSecret)

	secret, _ := stubSecret(context.Background())
	tok, _ := auth.Sign(secret, auth.Claims{
		UserID:     "u-test",
		SystemRole: system.RoleAdmin,
		Issued:     time.Now().Add(-2 * time.Hour),
		Expiry:     time.Now().Add(-time.Hour),
	})

	req := httptest.NewRequest(http.MethodGet, "/users/admin", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}
