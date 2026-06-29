// Package client is a Go HTTP client for the astroclaw API. It handles
// authentication (login + bearer token) and translates HTTP responses
// into typed Go values and sentinel errors.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"astroclaw/pkg/app/chat"
	"astroclaw/pkg/app/settings"
	"astroclaw/pkg/app/system"
)

// Client talks to two astroclaw endpoints: the API Gateway base URL for
// short CRUD calls, and an optional reply URL for the Reply Lambda's
// Function URL (which has a 15 minute timeout instead of the 30 second
// API Gateway limit). After Login, the returned token is held in memory
// and attached to every subsequent request.
//
// Client is safe for concurrent use after construction.
type Client struct {
	baseURL    string
	replyURL   string
	httpc      *http.Client
	replyHttpc *http.Client
	token      string
}

// New returns a Client that talks to baseURL (e.g. "https://api.example.com").
// The default HTTP timeout is 30s for the API and 15 minutes for reply
// calls (the Lambda hard limit). Call SetReplyURL before using Chat.
func New(baseURL string) *Client {
	return &Client{
		baseURL:    baseURL,
		httpc:      &http.Client{Timeout: 30 * time.Second},
		replyHttpc: &http.Client{Timeout: 15 * time.Minute},
	}
}

// SetToken stores a bearer token to be attached to subsequent requests.
func (c *Client) SetToken(tok string) {
	c.token = tok
}

// SetReplyURL sets the URL of the Reply Lambda Function URL. Required
// before calling Chat. CLIs typically read it from env ASTROCLAW_REPLY_URL.
func (c *Client) SetReplyURL(url string) {
	c.replyURL = url
}

// Token returns the current bearer token, or "" if no Login has succeeded
// and SetToken has not been called.
func (c *Client) Token() string {
	return c.token
}

// do issues a request against the API base URL with the standard 30s
// timeout. Most methods use this.
func (c *Client) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	return c.doAt(ctx, c.httpc, c.baseURL, method, path, body)
}

// doAt is the shared HTTP roundtrip implementation. It encodes the body,
// attaches the bearer token, sends the request via the supplied client,
// and maps non-2xx responses to sentinel errors. Chat uses replyHttpc
// and replyURL; everything else uses httpc and baseURL.
func (c *Client) doAt(ctx context.Context, hc *http.Client, baseURL, method, path string, body any) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("client: encode body: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("client: build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("client: request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("client: read response: %w", err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return respBody, nil
	}
	return nil, errorFromResponse(resp.StatusCode, respBody)
}

// errorFromResponse maps a non-2xx status + body to a sentinel-wrapped
// error so callers can use errors.Is to make decisions.
func errorFromResponse(status int, body []byte) error {
	msg := serverErrorMessage(body)
	switch {
	case status == http.StatusUnauthorized:
		return fmt.Errorf("%w: %s", ErrUnauthorized, msg)
	case status == http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrNotFound, msg)
	case status >= 500:
		return fmt.Errorf("%w: %s", ErrServer, msg)
	default:
		return fmt.Errorf("client: http %d: %s", status, msg)
	}
}

// serverErrorMessage pulls the "error" field out of our handlers'
// standard {"error": "..."} JSON body. Falls back to the raw bytes if
// the body is not JSON or has no error field.
func serverErrorMessage(body []byte) string {
	var resp struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &resp) == nil && resp.Error != "" {
		return resp.Error
	}
	return string(body)
}

// LoginResponse is the body of a successful POST /login.
type LoginResponse struct {
	Token string       `json:"token"`
	User  *system.User `json:"user"`
}

// Login authenticates with email and password. On success the token is
// stored in the Client and attached to all subsequent requests; callers
// who need to persist it across processes can read it back with Token.
//
// Wrong credentials surface as ErrInvalidCredentials so the CLI can
// distinguish that case from a stale or invalid bearer token.
func (c *Client) Login(ctx context.Context, email, password string) (*LoginResponse, error) {
	body, err := c.do(ctx, http.MethodPost, "/login", map[string]string{
		"email":    email,
		"password": password,
	})
	if err != nil {
		if errors.Is(err, ErrUnauthorized) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	var resp LoginResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("client: decode login response: %w", err)
	}
	c.token = resp.Token
	return &resp, nil
}

// GetAdmin returns the platform admin user.
func (c *Client) GetAdmin(ctx context.Context) (*system.User, error) {
	body, err := c.do(ctx, http.MethodGet, "/users/admin", nil)
	if err != nil {
		return nil, err
	}
	var u system.User
	if err := json.Unmarshal(body, &u); err != nil {
		return nil, fmt.Errorf("client: decode user: %w", err)
	}
	return &u, nil
}

// ListUserWorkspaces returns all workspaces the given user belongs to.
func (c *Client) ListUserWorkspaces(ctx context.Context, userID string) ([]*system.Workspace, error) {
	body, err := c.do(ctx, http.MethodGet, "/users/"+userID+"/workspaces", nil)
	if err != nil {
		return nil, err
	}
	var ws []*system.Workspace
	if err := json.Unmarshal(body, &ws); err != nil {
		return nil, fmt.Errorf("client: decode workspaces: %w", err)
	}
	return ws, nil
}

// Me returns the currently authenticated user, identified by the bearer
// token. CLIs use this to discover the user's ID without keeping it in
// local state.
func (c *Client) Me(ctx context.Context) (*system.User, error) {
	body, err := c.do(ctx, http.MethodGet, "/me", nil)
	if err != nil {
		return nil, err
	}
	var u system.User
	if err := json.Unmarshal(body, &u); err != nil {
		return nil, fmt.Errorf("client: decode me: %w", err)
	}
	return &u, nil
}

// GetWorkspaceSetting returns a workspace-scoped setting by name.
func (c *Client) GetWorkspaceSetting(ctx context.Context, workspaceID, name string) (*settings.WorkspaceSetting, error) {
	body, err := c.do(ctx, http.MethodGet, "/workspaces/"+workspaceID+"/settings/"+name, nil)
	if err != nil {
		return nil, err
	}
	var s settings.WorkspaceSetting
	if err := json.Unmarshal(body, &s); err != nil {
		return nil, fmt.Errorf("client: decode setting: %w", err)
	}
	return &s, nil
}

// CreateSession creates a new chat session in the given workspace owned
// by userID and attached to one or more agents.
func (c *Client) CreateSession(ctx context.Context, workspaceID, userID string, agentIDs []string, title string) (*chat.Session, error) {
	body, err := c.do(ctx, http.MethodPost, "/workspaces/"+workspaceID+"/sessions", map[string]any{
		"user_id":   userID,
		"agent_ids": agentIDs,
		"title":     title,
	})
	if err != nil {
		return nil, err
	}
	var s chat.Session
	if err := json.Unmarshal(body, &s); err != nil {
		return nil, fmt.Errorf("client: decode session: %w", err)
	}
	return &s, nil
}

// Chat sends a message to a session and waits for the final reply.
// The reply Lambda streams intermediate state via WebSocket; Chat
// ignores that stream and returns only the final reply text from the
// synchronous HTTP response.
func (c *Client) Chat(ctx context.Context, sessionID, agentID, text string) (string, error) {
	if c.replyURL == "" {
		return "", fmt.Errorf("client: reply URL not set, call SetReplyURL first")
	}
	body, err := c.doAt(ctx, c.replyHttpc, c.replyURL, http.MethodPost, "/sessions/"+sessionID+"/reply", map[string]string{
		"text":     text,
		"agent_id": agentID,
	})
	if err != nil {
		return "", err
	}
	var r struct {
		Reply string `json:"reply"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("client: decode reply: %w", err)
	}
	return r.Reply, nil
}
