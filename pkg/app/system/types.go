package system

import (
	"errors"
	"time"

	"iclaw/pkg/app/system/db"
)

var (
	ErrNotFound = errors.New("not found")
)

// User role constants.
const (
	RoleOwner = "owner"
	RoleGuest = "guest"
)

type User struct {
	ID        string
	Email     string
	Name      string
	Role      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func UserFromDB(u db.AppSystemUser) *User {
	return &User{
		ID:        u.ID,
		Email:     u.Email,
		Name:      u.Name,
		Role:      u.Role,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
	}
}

type Connection struct {
	ConnectionID string
	UserID       string
	ConnectedAt  time.Time
}

func ConnectionFromDB(c db.AppSystemConnection) *Connection {
	return &Connection{
		ConnectionID: c.ConnectionID,
		UserID:       c.UserID,
		ConnectedAt:  c.ConnectedAt,
	}
}
