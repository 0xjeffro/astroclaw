package passwords

import (
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("credential not found")
)

// System-scope credential names. Defined here so every caller references
// the same string and a typo can't silently create an orphan row.
const (
	SystemCredAnthropicAPIKey = "anthropic-api-key"
	SystemCredJWTSecret       = "jwt-secret"
)

// Credential is the decrypted form of a stored credential.
type Credential struct {
	Name        string
	Description string
	Value       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// CredentialMetadata is what List endpoints return: no plaintext, no
// ciphertext, just the user-facing metadata.
type CredentialMetadata struct {
	Name        string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
