package passwords

import (
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("credential not found")
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
