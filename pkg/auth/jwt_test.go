package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"astroclaw/pkg/app/system"

	"github.com/golang-jwt/jwt/v5"
)

// testSecret is 32 bytes of fixed content, long enough to pass the
// minimum-length check. Real deployments use random bytes from crypto/rand.
var testSecret = []byte("0123456789abcdef0123456789abcdef")

func validClaims() Claims {
	now := time.Now()
	return Claims{
		UserID:     "user-uuid-123",
		SystemRole: system.RoleAdmin,
		Issued:     now,
		Expiry:     now.Add(time.Hour),
	}
}

// Round-trip: a token produced by Sign must Parse back into the same
// logical claims. Times are compared with truncation to one second since
// JWT encodes Unix seconds, dropping sub-second precision.
func TestSignParseRoundTrip(t *testing.T) {
	c := validClaims()
	tok, err := Sign(testSecret, c)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	got, err := Parse(testSecret, tok)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if got.UserID != c.UserID {
		t.Errorf("UserID: got %q, want %q", got.UserID, c.UserID)
	}
	if got.SystemRole != c.SystemRole {
		t.Errorf("SystemRole: got %q, want %q", got.SystemRole, c.SystemRole)
	}
	if !got.Issued.Equal(c.Issued.Truncate(time.Second)) {
		t.Errorf("Issued: got %v, want %v", got.Issued, c.Issued.Truncate(time.Second))
	}
	if !got.Expiry.Equal(c.Expiry.Truncate(time.Second)) {
		t.Errorf("Expiry: got %v, want %v", got.Expiry, c.Expiry.Truncate(time.Second))
	}
}

// Expired token: Parse must surface ErrTokenExpired specifically, so the
// middleware can distinguish "log in again" from "this token was always bad".
func TestParseExpired(t *testing.T) {
	c := validClaims()
	c.Issued = time.Now().Add(-2 * time.Hour)
	c.Expiry = time.Now().Add(-time.Hour)

	tok, err := Sign(testSecret, c)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	_, err = Parse(testSecret, tok)
	if !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("expected ErrTokenExpired, got %v", err)
	}
}

// alg=none attack: an attacker crafts a token with no signature. Parse must
// refuse it; otherwise anyone could forge claims. This is the canonical
// JWT vulnerability the algorithm-family check defends against.
func TestParseRejectsAlgNone(t *testing.T) {
	c := validClaims()
	unsigned := jwt.NewWithClaims(jwt.SigningMethodNone, jwtClaims{
		Role: string(c.SystemRole),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   c.UserID,
			IssuedAt:  jwt.NewNumericDate(c.Issued),
			ExpiresAt: jwt.NewNumericDate(c.Expiry),
		},
	})
	tokStr, err := unsigned.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("build alg=none token: %v", err)
	}

	if _, err := Parse(testSecret, tokStr); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid for alg=none, got %v", err)
	}
}

// Wrong secret: Parse with a different secret must fail because the HMAC
// signature won't match. Catches "someone reused a key they shouldn't have".
func TestParseRejectsWrongSecret(t *testing.T) {
	tok, err := Sign(testSecret, validClaims())
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	wrong := []byte("ffffffffffffffffffffffffffffffff")
	if _, err := Parse(wrong, tok); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid for wrong secret, got %v", err)
	}
}

// Tampered payload: flip a byte in the middle base64 segment. The
// signature won't match the modified payload and Parse must reject.
func TestParseRejectsTamperedPayload(t *testing.T) {
	tok, err := Sign(testSecret, validClaims())
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	// Token format is header.payload.signature; corrupt one character of the payload.
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("unexpected token format: %s", tok)
	}
	parts[1] = parts[1][:len(parts[1])-1] + "A"
	tampered := strings.Join(parts, ".")

	if _, err := Parse(testSecret, tampered); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid for tampered token, got %v", err)
	}
}

// Short secrets are refused by both Sign and Parse so a misconfigured
// deployment can't silently produce weak HMACs.
func TestRejectsShortSecret(t *testing.T) {
	short := []byte("too-short")
	if _, err := Sign(short, validClaims()); err == nil {
		t.Error("Sign should refuse a <32-byte secret")
	}
	if _, err := Parse(short, "ignored"); err == nil {
		t.Error("Parse should refuse a <32-byte secret")
	}
}
