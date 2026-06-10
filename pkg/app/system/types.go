package system

import (
	"errors"
	"time"

	"astroclaw/pkg/app/system/db"
)

var (
	ErrNotFound = errors.New("not found")

	// ErrInvalidCredentials is returned by VerifyUserPassword when the email
	// is unknown or the password does not match. The two cases collapse into
	// one error to avoid leaking which emails are registered.
	ErrInvalidCredentials = errors.New("invalid credentials")
)

// Platform-level roles (app_system_users.role).
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// Workspace-level roles (app_system_memberships.role).
const (
	WorkspaceRoleOwner  = "owner"
	WorkspaceRoleMember = "member"
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

type Workspace struct {
	ID        string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

func WorkspaceFromDB(w db.AppSystemWorkspace) *Workspace {
	return &Workspace{
		ID:        w.ID,
		Name:      w.Name,
		CreatedAt: w.CreatedAt,
		UpdatedAt: w.UpdatedAt,
		DeletedAt: w.DeletedAt,
	}
}

type Membership struct {
	UserID      string
	WorkspaceID string
	Role        string
	JoinedAt    time.Time
}

func MembershipFromDB(m db.AppSystemWorkspaceMember) *Membership {
	return &Membership{
		UserID:      m.UserID,
		WorkspaceID: m.WorkspaceID,
		Role:        m.Role,
		JoinedAt:    m.JoinedAt,
	}
}

// WorkspaceMember pairs a user with their role in a specific workspace.
type WorkspaceMember struct {
	User          *User
	WorkspaceRole string
	JoinedAt      time.Time
}

func WorkspaceMemberFromDB(r db.ListMembersByWorkspaceRow) *WorkspaceMember {
	return &WorkspaceMember{
		User: &User{
			ID:        r.ID,
			Email:     r.Email,
			Name:      r.Name,
			Role:      r.Role,
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
		},
		WorkspaceRole: r.WorkspaceRole,
		JoinedAt:      r.JoinedAt,
	}
}

type Connection struct {
	ConnectionID string
	UserID       string
	WorkspaceID  string
	ConnectedAt  time.Time
}

func ConnectionFromDB(c db.AppSystemConnection) *Connection {
	return &Connection{
		ConnectionID: c.ConnectionID,
		UserID:       c.UserID,
		WorkspaceID:  c.WorkspaceID,
		ConnectedAt:  c.ConnectedAt,
	}
}
