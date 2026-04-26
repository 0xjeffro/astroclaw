package passwords

import (
	"errors"
	"time"

	"iclaw/pkg/app/passwords/db"
)

var (
	ErrNotFound = errors.New("credential not found")
)

type Credential struct {
	ID          string
	Name        string
	Value       string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time
}

// CredentialFromDB constructs a passwords.Credential from a db.Credential.
func CredentialFromDB(c db.Credential) *Credential {
	return &Credential{
		ID:          c.ID,
		Name:        c.Name,
		Value:       c.Value,
		Description: c.Description,
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
		DeletedAt:   c.DeletedAt,
	}
}
