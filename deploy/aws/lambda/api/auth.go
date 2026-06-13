package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"astroclaw/pkg/app/system"
	"astroclaw/pkg/auth"
)

// tokenTTL is how long an issued session token stays valid. 24h is a
// common default: short enough that a leaked token expires within a day,
// long enough that users do not re-login mid-session.
const tokenTTL = 24 * time.Hour

// login handles POST /login. It verifies the supplied credentials and
// returns a signed JWT plus the public user record.
//
// The response collapses "unknown email" and "wrong password" into a
// single 401 to avoid leaking which emails are registered.
func login(svc systemService, getSecret func(context.Context) ([]byte, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
		if body.Email == "" || body.Password == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email and password required"})
			return
		}

		user, err := svc.VerifyUserPassword(r.Context(), body.Email, body.Password)
		if err != nil {
			if errors.Is(err, system.ErrInvalidCredentials) {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "login failed"})
			return
		}

		secret, err := getSecret(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "auth misconfigured"})
			return
		}

		now := time.Now()
		tok, err := auth.Sign(secret, auth.Claims{
			UserID:     user.ID,
			SystemRole: system.Role(user.Role),
			Issued:     now,
			Expiry:     now.Add(tokenTTL),
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "sign failed"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"token": tok,
			"user":  user,
		})
	}
}
