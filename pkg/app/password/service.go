package password

import (
	"context"
	"fmt"

	"iclaw/pkg/app/password/db"

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

func (svc *Service) CreateCredential(ctx context.Context, name, value, description string) (*Credential, error) {
	c, err := svc.queries.CreateCredential(ctx, db.CreateCredentialParams{
		Name:        name,
		Value:       value,
		Description: description,
	})
	if err != nil {
		return nil, fmt.Errorf("create credential: %w", err)
	}
	return CredentialFromDB(c), nil
}

func (svc *Service) GetCredentialByName(ctx context.Context, name string) (*Credential, error) {
	c, err := svc.queries.GetCredentialByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("credential %q: %w", name, ErrNotFound)
	}
	return CredentialFromDB(c), nil
}
