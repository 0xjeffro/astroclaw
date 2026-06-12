package auth

import (
	"astroclaw/pkg/app/system"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Sentinel errors so callers (middleware, login handler) can distinguish
// between expired tokens and otherwise invalid ones.
var (
	ErrTokenExpired = errors.New("auth: token expired")
	ErrTokenInvalid = errors.New("auth: token invalid")
)

type Claims struct {
	UserID     string
	SystemRole system.Role
	Issued     time.Time
	Expiry     time.Time
}

type jwtClaims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

func Sign(secret []byte, c Claims) (string, error) {
	if len(secret) < 32 {
		return "", fmt.Errorf("jwt secret too short: %d bytes, need >=32", len(secret))
	}
	claims := jwtClaims{
		Role: string(c.SystemRole),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   c.UserID,
			IssuedAt:  jwt.NewNumericDate(c.Issued),
			ExpiresAt: jwt.NewNumericDate(c.Expiry),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}

// Parse verifies a token against the secret and returns the decoded claims.
func Parse(secret []byte, tokenStr string) (*Claims, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("jwt secret too short: %d bytes, need >=32", len(secret))
	}

	var raw jwtClaims
	token, err := jwt.ParseWithClaims(tokenStr, &raw, func(t *jwt.Token) (any, error) {
		// Reject any algorithm outside the HMAC family. Without this check
		// an attacker could forge a token with alg=none and the library
		// would accept the unsigned payload.
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return secret, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, fmt.Errorf("%w: %v", ErrTokenInvalid, err)
	}
	if !token.Valid {
		return nil, ErrTokenInvalid
	}

	// Defensive type check
	if raw.Subject == "" || raw.IssuedAt == nil || raw.ExpiresAt == nil {
		return nil, ErrTokenInvalid
	}

	return &Claims{
		UserID:     raw.Subject,
		SystemRole: system.Role(raw.Role),
		Issued:     raw.IssuedAt.Time,
		Expiry:     raw.ExpiresAt.Time,
	}, nil
}
