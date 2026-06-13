package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"astroclaw/pkg/app/system"
	"astroclaw/pkg/auth"
)

// failingSecret simulates a SecretLoader.Get that cannot load the key.
func failingSecret(_ context.Context) ([]byte, error) {
	return nil, errors.New("kms throttled")
}

// Happy path: valid credentials produce a token that parses back to the
// same user, plus a JSON body with token + user.
func TestLoginValid(t *testing.T) {
	sys := &fakeSystem{verifyUser: &system.User{
		ID:    "u-123",
		Email: "admin@example.com",
		Role:  string(system.RoleAdmin),
	}}
	r := newRouter(&fakeChat{}, &fakeSettings{}, sys, stubSecret)

	w := do(t, r, http.MethodPost, "/login", map[string]string{
		"email":    "admin@example.com",
		"password": "hunter2",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if sys.verifyCalledEmail != "admin@example.com" || sys.verifyCalledPassword != "hunter2" {
		t.Errorf("VerifyUserPassword args: got (%q, %q)", sys.verifyCalledEmail, sys.verifyCalledPassword)
	}

	var resp struct {
		Token string       `json:"token"`
		User  *system.User `json:"user"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("token missing from response")
	}
	if resp.User == nil || resp.User.ID != "u-123" {
		t.Errorf("user in response: got %+v", resp.User)
	}

	// Token should round-trip through Parse with the same secret.
	secret, _ := stubSecret(context.Background())
	claims, err := auth.Parse(secret, resp.Token)
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	if claims.UserID != "u-123" || claims.SystemRole != system.RoleAdmin {
		t.Errorf("token claims: got %+v", claims)
	}
}

// Wrong credentials surface as 401 with a generic message; no token is
// issued and the response says nothing about which field was wrong.
func TestLoginInvalidCredentials(t *testing.T) {
	sys := &fakeSystem{verifyErr: system.ErrInvalidCredentials}
	r := newRouter(&fakeChat{}, &fakeSettings{}, sys, stubSecret)

	w := do(t, r, http.MethodPost, "/login", map[string]string{
		"email":    "anyone@example.com",
		"password": "whatever",
	})

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
	var resp struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Error != "invalid credentials" {
		t.Errorf("error msg: got %q", resp.Error)
	}
}

// Malformed JSON body is a 400 before we ever talk to the service.
func TestLoginBadBody(t *testing.T) {
	sys := &fakeSystem{}
	r := newRouter(&fakeChat{}, &fakeSettings{}, sys, stubSecret)

	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("{not-json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	if sys.verifyCalledEmail != "" {
		t.Error("VerifyUserPassword should not be called for bad body")
	}
}

// Missing email or password is a 400 with a distinct message.
func TestLoginMissingFields(t *testing.T) {
	sys := &fakeSystem{}
	r := newRouter(&fakeChat{}, &fakeSettings{}, sys, stubSecret)

	w := do(t, r, http.MethodPost, "/login", map[string]string{
		"email":    "admin@example.com",
		"password": "",
	})

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	var resp struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Error != "email and password required" {
		t.Errorf("error msg: got %q", resp.Error)
	}
}

// Secret loader failure after a successful password check is a 500: the
// client did everything right, the server cannot finish the job.
func TestLoginSecretLoadError(t *testing.T) {
	sys := &fakeSystem{verifyUser: &system.User{ID: "u-1", Role: string(system.RoleAdmin)}}
	r := newRouter(&fakeChat{}, &fakeSettings{}, sys, failingSecret)

	w := do(t, r, http.MethodPost, "/login", map[string]string{
		"email":    "x@example.com",
		"password": "ok",
	})

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
	var resp struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Error != "auth misconfigured" {
		t.Errorf("error msg: got %q", resp.Error)
	}
}
