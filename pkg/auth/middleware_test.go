package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var mwTestSecret = []byte("0123456789abcdef0123456789abcdef")

// In RequireAuth, we pass getSecret to retrieve secret from DB + KMS,
// but in test we don't want to relay on DB or KMS, so we mock this:
// okSecret: always returns the same test secret, simulating a healthy secret loader.
// badSecret: it always fails with a KMS error, simulating a bad secret loader.
func okSecret(_ context.Context) ([]byte, error) { return mwTestSecret, nil }

func badSecret(_ context.Context) ([]byte, error) {
	return nil, errors.New("kms throttled")
}

// signTestToken builds a signed token with the given claims, failing the
// test on any signing error.
func signTestToken(t *testing.T, c Claims) string {
	t.Helper()
	tok, err := Sign(mwTestSecret, c)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return tok
}

// recordingHandler is an http.Handler that records whether it was invoked
// and what claims were on the context when it ran.
type recordingHandler struct {
	called bool
	claims *Claims
}

func (h *recordingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.called = true
	if c, ok := ClaimsFromContext(r.Context()); ok {
		h.claims = c
	}
	w.WriteHeader(http.StatusOK)
}

// decodeErr pulls the "error" field out of a writeAuthError JSON body.
func decodeErr(t *testing.T, body []byte) string {
	t.Helper()
	var resp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode body %q: %v", string(body), err)
	}
	return resp.Error
}

// Happy path: a valid token passes through and the handler sees the claims.
func TestRequireAuthValid(t *testing.T) {
	rec := &recordingHandler{}
	mw := RequireAuth(okSecret)(rec)

	tok := signTestToken(t, validClaims())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()

	mw.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	if !rec.called {
		t.Fatal("next handler was not called")
	}
	if rec.claims == nil || rec.claims.UserID != "user-uuid-123" {
		t.Errorf("claims on ctx: got %+v", rec.claims)
	}
}

// No Authorization header at all: 401 and next must not run.
func TestRequireAuthMissingHeader(t *testing.T) {
	rec := &recordingHandler{}
	mw := RequireAuth(okSecret)(rec)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", w.Code)
	}
	if msg := decodeErr(t, w.Body.Bytes()); msg != "missing bearer token" {
		t.Errorf("error msg: got %q", msg)
	}
	if rec.called {
		t.Error("next handler was called on missing header")
	}
}

// Non-Bearer scheme is rejected with the same 401.
func TestRequireAuthWrongScheme(t *testing.T) {
	rec := &recordingHandler{}
	mw := RequireAuth(okSecret)(rec)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", w.Code)
	}
	if msg := decodeErr(t, w.Body.Bytes()); msg != "missing bearer token" {
		t.Errorf("error msg: got %q", msg)
	}
	if rec.called {
		t.Error("next handler was called on non-bearer scheme")
	}
}

// Expired token: 401 with "token expired" so the client can distinguish
// "log in again" from "this token was always bad".
func TestRequireAuthExpired(t *testing.T) {
	rec := &recordingHandler{}
	mw := RequireAuth(okSecret)(rec)

	c := validClaims()
	c.Issued = time.Now().Add(-2 * time.Hour)
	c.Expiry = time.Now().Add(-time.Hour)
	tok := signTestToken(t, c)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", w.Code)
	}
	if msg := decodeErr(t, w.Body.Bytes()); msg != "token expired" {
		t.Errorf("error msg: got %q", msg)
	}
}

// Tampered payload: signature won't verify, falls into the generic invalid
// bucket.
func TestRequireAuthInvalid(t *testing.T) {
	rec := &recordingHandler{}
	mw := RequireAuth(okSecret)(rec)

	tok := signTestToken(t, validClaims())
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("unexpected token format: %s", tok)
	}
	parts[1] = parts[1][:len(parts[1])-1] + "A"
	tampered := strings.Join(parts, ".")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tampered)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", w.Code)
	}
	if msg := decodeErr(t, w.Body.Bytes()); msg != "invalid token" {
		t.Errorf("error msg: got %q", msg)
	}
}

// Secret loader failure: 500 (server fault, not client fault) and next
// must not run.
func TestRequireAuthSecretLoadError(t *testing.T) {
	rec := &recordingHandler{}
	mw := RequireAuth(badSecret)(rec)

	tok := signTestToken(t, validClaims())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", w.Code)
	}
	if msg := decodeErr(t, w.Body.Bytes()); msg != "auth misconfigured" {
		t.Errorf("error msg: got %q", msg)
	}
	if rec.called {
		t.Error("next handler was called despite secret load error")
	}
}

// Empty context returns (nil, false), not a panic or zero-Claims pointer.
func TestClaimsFromContextMissing(t *testing.T) {
	c, ok := ClaimsFromContext(context.Background())
	if ok || c != nil {
		t.Errorf("got (%v, %v), want (nil, false)", c, ok)
	}
}

func TestUserIDFromContextMissing(t *testing.T) {
	id, ok := UserIDFromContext(context.Background())
	if ok || id != "" {
		t.Errorf("got (%q, %v), want (\"\", false)", id, ok)
	}
}

// Unit test for the package-private bearerToken helper. Covers the
// non-obvious cases: empty header, wrong scheme, exact-prefix collision.
func TestBearerTokenExtraction(t *testing.T) {
	cases := []struct {
		header  string
		wantTok string
		wantOK  bool
	}{
		{"", "", false},
		{"Bearer abc.def.ghi", "abc.def.ghi", true},
		{"Basic dXNlcjpwYXNz", "", false},
		{"Bearerabc", "", false},
		{"bearer abc", "", false},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if tc.header != "" {
			req.Header.Set("Authorization", tc.header)
		}
		gotTok, gotOK := bearerToken(req)
		if gotTok != tc.wantTok || gotOK != tc.wantOK {
			t.Errorf("header %q: got (%q, %v), want (%q, %v)",
				tc.header, gotTok, gotOK, tc.wantTok, tc.wantOK)
		}
	}
}
