package system

import (
	"context"
	"fmt"

	"iclaw/pkg/app/system/db"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	queries *db.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{queries: db.New(pool)}
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
	return UserFromDB(u), nil
}

func (svc *Service) GetOwner(ctx context.Context) (*User, error) {
	u, err := svc.queries.GetOwner(ctx)
	if err != nil {
		return nil, fmt.Errorf("owner not found: %w", err)
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

// Connections

func (svc *Service) CreateWSConnectRecord(ctx context.Context, connectionID, userID string) error {
	return svc.queries.CreateConnection(ctx, db.CreateConnectionParams{
		ConnectionID: connectionID,
		UserID:       userID,
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
