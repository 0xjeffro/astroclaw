package system

import (
	"context"
	"fmt"

	"astroclaw/pkg/app/system/db"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DataKeyProvisioner provisions an encryption data key for a newly created
// workspace or user, so the passwords app can later encrypt credentials
// scoped to that entity. Implemented by *passwords.Service.
//
// system declares this interface (rather than importing passwords) to keep
// the dependency direction one-way: orchestration code wires passwords
// into system, but the passwords package never needs to know about system.
type DataKeyProvisioner interface {
	ProvisionWorkspaceDataKey(ctx context.Context, workspaceID string) error
	ProvisionUserDataKey(ctx context.Context, userID string) error
}

type Service struct {
	queries     *db.Queries
	provisioner DataKeyProvisioner
}

type Option func(*Service)

// WithProvisioner attaches a DataKeyProvisioner. Required for any caller
// that creates users or workspaces because we need to create data key for them;
func WithProvisioner(p DataKeyProvisioner) Option {
	return func(s *Service) { s.provisioner = p }
}

// NewService returns a system Service.
//
// Callers that create users or workspaces must pass WithProvisioner so
// the corresponding passwords data key is created in the same flow.
// Lambdas that only read or delete (e.g. wsdisconnect) can omit it.
//
// TODO: thread the provisioner call through the same DB transaction as
// the workspace/user insert, so a provisioning failure rolls back the
// entity creation.
func NewService(pool *pgxpool.Pool, opts ...Option) *Service {
	svc := &Service{queries: db.New(pool)}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

// Users

func (svc *Service) CreateUser(ctx context.Context, email, name, role string) (*User, error) {
	u, err := svc.queries.CreateUser(ctx, db.CreateUserParams{
		Email: email,
		Name:  name,
		Role:  role,
	})
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	if svc.provisioner != nil {
		if err := svc.provisioner.ProvisionUserDataKey(ctx, u.ID); err != nil {
			return nil, fmt.Errorf("provision data key for user %q: %w", u.ID, err)
		}
	}

	return UserFromDB(u), nil
}

func (svc *Service) GetAdmin(ctx context.Context) (*User, error) {
	u, err := svc.queries.GetAdmin(ctx)
	if err != nil {
		return nil, fmt.Errorf("admin not found: %w", err)
	}
	return UserFromDB(u), nil
}

func (svc *Service) GetUser(ctx context.Context, id string) (*User, error) {
	u, err := svc.queries.GetUser(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("user %q: %w", id, ErrNotFound)
	}
	return UserFromDB(u), nil
}

func (svc *Service) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	u, err := svc.queries.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("user with email %q: %w", email, ErrNotFound)
	}
	return UserFromDB(u), nil
}

func (svc *Service) ListUsers(ctx context.Context) ([]*User, error) {
	rows, err := svc.queries.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	users := make([]*User, len(rows))
	for i, u := range rows {
		users[i] = UserFromDB(u)
	}
	return users, nil
}

// Workspaces

func (svc *Service) CreateWorkspace(ctx context.Context, name string) (*Workspace, error) {
	w, err := svc.queries.CreateWorkspace(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("create workspace: %w", err)
	}
	if svc.provisioner != nil {
		if err := svc.provisioner.ProvisionWorkspaceDataKey(ctx, w.ID); err != nil {
			return nil, fmt.Errorf("provision data key for workspace %q: %w", w.ID, err)
		}
	}
	return WorkspaceFromDB(w), nil
}

func (svc *Service) GetWorkspace(ctx context.Context, id string) (*Workspace, error) {
	w, err := svc.queries.GetWorkspace(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("workspace %q: %w", id, ErrNotFound)
	}
	return WorkspaceFromDB(w), nil
}

func (svc *Service) ListWorkspaces(ctx context.Context) ([]*Workspace, error) {
	rows, err := svc.queries.ListWorkspaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	out := make([]*Workspace, len(rows))
	for i, w := range rows {
		out[i] = WorkspaceFromDB(w)
	}
	return out, nil
}

func (svc *Service) ListWorkspacesForUser(ctx context.Context, userID string) ([]*Workspace, error) {
	rows, err := svc.queries.ListWorkspacesForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list workspaces for user %q: %w", userID, err)
	}
	out := make([]*Workspace, len(rows))
	for i, w := range rows {
		out[i] = WorkspaceFromDB(w)
	}
	return out, nil
}

func (svc *Service) RenameWorkspace(ctx context.Context, id, name string) error {
	return svc.queries.UpdateWorkspaceName(ctx, db.UpdateWorkspaceNameParams{
		ID:   id,
		Name: name,
	})
}

func (svc *Service) DeleteWorkspace(ctx context.Context, id string) error {
	return svc.queries.SoftDeleteWorkspace(ctx, id)
}

// Memberships

func (svc *Service) AddMembership(ctx context.Context, userID, workspaceID, role string) error {
	return svc.queries.AddMembership(ctx, db.AddMembershipParams{
		UserID:      userID,
		WorkspaceID: workspaceID,
		Role:        role,
	})
}

func (svc *Service) RemoveMembership(ctx context.Context, userID, workspaceID string) error {
	return svc.queries.RemoveMembership(ctx, db.RemoveMembershipParams{
		UserID:      userID,
		WorkspaceID: workspaceID,
	})
}

func (svc *Service) GetMembership(ctx context.Context, userID, workspaceID string) (*Membership, error) {
	m, err := svc.queries.GetMembership(ctx, db.GetMembershipParams{
		UserID:      userID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return nil, fmt.Errorf("membership user=%q workspace=%q: %w", userID, workspaceID, ErrNotFound)
	}
	return MembershipFromDB(m), nil
}

func (svc *Service) ListMembersByWorkspace(ctx context.Context, workspaceID string) ([]*WorkspaceMember, error) {
	rows, err := svc.queries.ListMembersByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list members for workspace %q: %w", workspaceID, err)
	}
	out := make([]*WorkspaceMember, len(rows))
	for i, r := range rows {
		out[i] = WorkspaceMemberFromDB(r)
	}
	return out, nil
}

func (svc *Service) UpdateMembershipRole(ctx context.Context, userID, workspaceID, role string) error {
	return svc.queries.UpdateMembershipRole(ctx, db.UpdateMembershipRoleParams{
		UserID:      userID,
		WorkspaceID: workspaceID,
		Role:        role,
	})
}

// Connections

func (svc *Service) CreateWSConnectRecord(ctx context.Context, connectionID, userID, workspaceID string) error {
	return svc.queries.CreateConnection(ctx, db.CreateConnectionParams{
		ConnectionID: connectionID,
		UserID:       userID,
		WorkspaceID:  workspaceID,
	})
}

func (svc *Service) DeleteWSConnectRecord(ctx context.Context, connectionID string) error {
	return svc.queries.DeleteConnection(ctx, connectionID)
}

func (svc *Service) GetConnectionsByUser(ctx context.Context, userID string) ([]*Connection, error) {
	rows, err := svc.queries.GetConnectionsByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get connections for user %q: %w", userID, err)
	}
	conns := make([]*Connection, len(rows))
	for i, c := range rows {
		conns[i] = ConnectionFromDB(c)
	}
	return conns, nil
}
