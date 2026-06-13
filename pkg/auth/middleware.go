package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// claimsKey is the context key under which we stash *Claims after a
// successful token verification. The unexported type ctxKey prevents
// other packages from accidentally writing to or reading from this slot.
type ctxKey struct{}

// claimsKey is the context key under which we stash *Claims
var claimsKey ctxKey

func ClaimsFromContext(ctx context.Context) (*Claims, bool) {
	c, ok := ctx.Value(claimsKey).(*Claims)
	return c, ok
}

func UserIDFromContext(ctx context.Context) (string, bool) {
	c, ok := ClaimsFromContext(ctx)
	if !ok {
		return "", false
	}
	return c.UserID, true
}

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	return strings.TrimPrefix(h, prefix), true
}

func writeAuthError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", "Bearer")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// RequireAuth returns a middleware that rejects any request without a
// valid bearer token. On success, the request's context carries the
// parsed *Claims, retrievable via ClaimsFromContext.
func RequireAuth(getSecret func(context.Context) ([]byte, error)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr, ok := bearerToken(r)
			if !ok {
				writeAuthError(w, http.StatusUnauthorized, "missing bearer token")
				return
			}

			secret, err := getSecret(r.Context())
			if err != nil {
				writeAuthError(w, http.StatusInternalServerError, "auth misconfigured")
				return
			}

			claims, err := Parse(secret, tokenStr)
			if err != nil {
				switch {
				case errors.Is(err, ErrTokenExpired):
					writeAuthError(w, http.StatusUnauthorized, "token expired")
				default:
					writeAuthError(w, http.StatusUnauthorized, "invalid token")
				}
				return
			}

			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
