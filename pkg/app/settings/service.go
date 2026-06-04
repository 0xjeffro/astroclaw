package settings

import (
	"context"
	"fmt"

	"astroclaw/pkg/app/settings/db"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	queries *db.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{queries: db.New(pool)}
}

// System scope

func (svc *Service) GetSystemSetting(ctx context.Context, name string) (*SystemSetting, error) {
	s, err := svc.queries.GetSystemSetting(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("system setting %q: %w", name, ErrNotFound)
	}
	return SystemSettingFromDB(s), nil
}

func (svc *Service) UpsertSystemSetting(ctx context.Context, name, value string) error {
	return svc.queries.UpsertSystemSetting(ctx, db.UpsertSystemSettingParams{
		Name:  name,
		Value: value,
	})
}

// Workspace scope

func (svc *Service) GetWorkspaceSetting(ctx context.Context, workspaceID, name string) (*WorkspaceSetting, error) {
	s, err := svc.queries.GetWorkspaceSetting(ctx, db.GetWorkspaceSettingParams{
		WorkspaceID: workspaceID,
		Name:        name,
	})
	if err != nil {
		return nil, fmt.Errorf("workspace setting %q: %w", name, ErrNotFound)
	}
	return WorkspaceSettingFromDB(s), nil
}

func (svc *Service) UpsertWorkspaceSetting(ctx context.Context, workspaceID, name, value string) error {
	return svc.queries.UpsertWorkspaceSetting(ctx, db.UpsertWorkspaceSettingParams{
		WorkspaceID: workspaceID,
		Name:        name,
		Value:       value,
	})
}

// User scope

func (svc *Service) GetUserSetting(ctx context.Context, userID, name string) (*UserSetting, error) {
	s, err := svc.queries.GetUserSetting(ctx, db.GetUserSettingParams{
		UserID: userID,
		Name:   name,
	})
	if err != nil {
		return nil, fmt.Errorf("user setting %q: %w", name, ErrNotFound)
	}
	return UserSettingFromDB(s), nil
}

func (svc *Service) UpsertUserSetting(ctx context.Context, userID, name, value string) error {
	return svc.queries.UpsertUserSetting(ctx, db.UpsertUserSettingParams{
		UserID: userID,
		Name:   name,
		Value:  value,
	})
}
