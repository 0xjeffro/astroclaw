package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"astroclaw/pkg/app/chat"
	"astroclaw/pkg/app/settings"
	"astroclaw/pkg/app/system"

	"github.com/go-chi/chi/v5"
)

// chatService captures the subset of chat.Service used by API handlers.
// Defining it here lets tests swap in a fake without spinning up DSQL.
type chatService interface {
	CreateSessionInWorkspace(ctx context.Context, workspaceID, userID string, agentIDs []string, title string) (*chat.Session, error)
	ListSessionsByWorkspace(ctx context.Context, workspaceID string) ([]*chat.Session, error)
	GetSession(ctx context.Context, id string) (*chat.Session, error)
	SoftDeleteSession(ctx context.Context, id string) error
}

// settingsService captures the subset of settings.Service used by API
// handlers, so tests can inject a fake instead of a real DSQL-backed service.
type settingsService interface {
	GetSystemSetting(ctx context.Context, name string) (*settings.SystemSetting, error)
	GetWorkspaceSetting(ctx context.Context, workspaceID, name string) (*settings.WorkspaceSetting, error)
	UpsertWorkspaceSetting(ctx context.Context, workspaceID, name, value string) error
}

// systemService captures the subset of system.Service used by API handlers,
// so tests can inject a fake instead of a real DSQL-backed service.
type systemService interface {
	GetAdmin(ctx context.Context) (*system.User, error)
	ListWorkspacesForUser(ctx context.Context, userID string) ([]*system.Workspace, error)
	VerifyUserPassword(ctx context.Context, email, password string) (*system.User, error)
}

// newRouter wires up the chi router with the supplied services. Kept
// separate from init() so tests can build a router without DSQL.
//
// getSecret is the JWT signing-key loader, threaded through to handlers
// that need to sign tokens (POST /login) or verify them (auth middleware,
// added in a later wiring step).
func newRouter(chatSvc chatService, settingsSvc settingsService, systemSvc systemService, getSecret func(context.Context) ([]byte, error)) http.Handler {
	r := chi.NewRouter()

	r.Post("/login", login(systemSvc, getSecret))

	r.Route("/workspaces/{workspaceID}", func(r chi.Router) {
		r.Route("/sessions", func(r chi.Router) {
			r.Post("/", createSessionInWorkspace(chatSvc))
			r.Get("/", listSessions(chatSvc))
			r.Get("/{sessionID}", getSession(chatSvc))
			r.Delete("/{sessionID}", deleteSession(chatSvc))
		})
		r.Route("/settings", func(r chi.Router) {
			r.Get("/{name}", getWorkspaceSetting(settingsSvc))
			r.Put("/{name}", upsertWorkspaceSetting(settingsSvc))
		})
	})

	r.Get("/settings/{name}", getSystemSetting(settingsSvc))
	r.Get("/users/admin", getAdmin(systemSvc))
	r.Get("/users/{userID}/workspaces", listUserWorkspaces(systemSvc))

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	})

	return r
}

// createSessionInWorkspace handles POST /workspaces/{workspaceID}/sessions.
// It decodes the request body and creates a new chat session in the workspace.
func createSessionInWorkspace(svc chatService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		workspaceID := chi.URLParam(r, "workspaceID")
		var body struct {
			UserID   string   `json:"user_id"`
			AgentIDs []string `json:"agent_ids"`
			Title    string   `json:"title"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
		s, err := svc.CreateSessionInWorkspace(r.Context(), workspaceID, body.UserID, body.AgentIDs, body.Title)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, s)
	}
}

// listSessions handles GET /workspaces/{workspaceID}/sessions.
// It returns all sessions belonging to the given workspace.
func listSessions(svc chatService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		workspaceID := chi.URLParam(r, "workspaceID")
		sessions, err := svc.ListSessionsByWorkspace(r.Context(), workspaceID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, sessions)
	}
}

// getSession handles GET /workspaces/{workspaceID}/sessions/{sessionID}.
// It returns a single session by ID, or 404 if it does not exist.
func getSession(svc chatService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "sessionID")
		s, err := svc.GetSession(r.Context(), id)
		if err != nil {
			writeJSON(w, statusForError(err, http.StatusInternalServerError), map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, s)
	}
}

// deleteSession handles DELETE /workspaces/{workspaceID}/sessions/{sessionID}.
// It soft-deletes the session identified by sessionID.
func deleteSession(svc chatService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "sessionID")
		if err := svc.SoftDeleteSession(r.Context(), id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

// getSystemSetting handles GET /settings/{name}.
// It returns a deploy-wide system setting by name.
func getSystemSetting(svc settingsService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		s, err := svc.GetSystemSetting(r.Context(), name)
		if err != nil {
			writeJSON(w, statusForError(err, http.StatusInternalServerError), map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, s)
	}
}

// getWorkspaceSetting handles GET /workspaces/{workspaceID}/settings/{name}.
// It returns a workspace-scoped setting by name.
func getWorkspaceSetting(svc settingsService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		workspaceID := chi.URLParam(r, "workspaceID")
		name := chi.URLParam(r, "name")
		s, err := svc.GetWorkspaceSetting(r.Context(), workspaceID, name)
		if err != nil {
			writeJSON(w, statusForError(err, http.StatusInternalServerError), map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, s)
	}
}

// upsertWorkspaceSetting handles PUT /workspaces/{workspaceID}/settings/{name}.
// It creates or updates a workspace-scoped setting with the provided value.
func upsertWorkspaceSetting(svc settingsService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		workspaceID := chi.URLParam(r, "workspaceID")
		name := chi.URLParam(r, "name")
		var body struct {
			Value string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
		if err := svc.UpsertWorkspaceSetting(r.Context(), workspaceID, name, body.Value); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// getAdmin handles GET /users/admin.
// It returns the platform admin user.
func getAdmin(svc systemService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := svc.GetAdmin(r.Context())
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, u)
	}
}

// listUserWorkspaces handles GET /users/{userID}/workspaces.
// It returns all workspaces the given user belongs to.
func listUserWorkspaces(svc systemService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := chi.URLParam(r, "userID")
		ws, err := svc.ListWorkspacesForUser(r.Context(), userID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, ws)
	}
}

// writeJSON serializes body as JSON and writes it to the response with the
// given status code and a JSON Content-Type header.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// statusForError maps known sentinel errors to HTTP status codes; otherwise
// falls back to fallback (typically 404 or 500 depending on the call site).
func statusForError(err error, fallback int) int {
	switch {
	case errors.Is(err, chat.ErrNotFound),
		errors.Is(err, settings.ErrNotFound):
		return http.StatusNotFound
	default:
		return fallback
	}
}
