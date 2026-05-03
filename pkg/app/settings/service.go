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

func (svc *Service) GetKVSetting(ctx context.Context, name string) (*KVSetting, error) {
	s, err := svc.queries.GetKVSetting(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("setting %q: %w", name, ErrNotFound)
	}
	return KVSettingFromDB(s), nil
}

// UpsertKVSetting creates a setting if it doesn't exist, or updates
// its value if it does.
func (svc *Service) UpsertKVSetting(ctx context.Context, name, value string) error {
	_, err := svc.queries.GetKVSetting(ctx, name)
	if err != nil {
		// Doesn't exist, create it.
		_, err = svc.queries.CreateKVSetting(ctx, db.CreateKVSettingParams{
			Name:  name,
			Value: value,
		})
		if err != nil {
			return fmt.Errorf("create setting %q: %w", name, err)
		}
		return nil
	}
	// Exists, update it.
	return svc.queries.UpdateKVSetting(ctx, db.UpdateKVSettingParams{
		Value: value,
		Name:  name,
	})
}
