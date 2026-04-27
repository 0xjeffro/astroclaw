package settings

import (
	"context"
	"fmt"

	"iclaw/pkg/app/settings/db"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	queries *db.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{
		queries: db.New(pool),
	}
}

func (svc *Service) GetPromptSetting(ctx context.Context, name string) (*PromptSetting, error) {
	s, err := svc.queries.GetPromptSetting(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("setting %q: %w", name, ErrNotFound)
	}
	return PromptSettingFromDB(s), nil
}

// UpsertPromptSetting creates a setting if it doesn't exist, or updates
// its value if it does.
func (svc *Service) UpsertPromptSetting(ctx context.Context, name, value string) error {
	_, err := svc.queries.GetPromptSetting(ctx, name)
	if err != nil {
		// Doesn't exist, create it.
		_, err = svc.queries.CreatePromptSetting(ctx, db.CreatePromptSettingParams{
			Name:  name,
			Value: value,
		})
		if err != nil {
			return fmt.Errorf("create setting %q: %w", name, err)
		}
		return nil
	}
	// Exists, update it.
	return svc.queries.UpdatePromptSetting(ctx, db.UpdatePromptSettingParams{
		Value: value,
		Name:  name,
	})
}
