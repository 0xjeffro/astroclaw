package skills

import (
	"context"
	"fmt"
	"strings"

	"astroclaw/pkg/app/skills/db"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	queries *db.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{queries: db.New(pool)}
}

func (svc *Service) CreateSkill(ctx context.Context, author, name, description, whenToUse string, tags []string, version string) (*Skill, error) {
	s, err := svc.queries.CreateSkill(ctx, db.CreateSkillParams{
		Author:      author,
		Name:        name,
		Description: description,
		WhenToUse:   whenToUse,
		Tags:        strings.Join(tags, ","),
		Version:     version,
	})
	if err != nil {
		return nil, fmt.Errorf("create skill: %w", err)
	}
	return SkillFromDB(s), nil
}

func (svc *Service) GetSkillByName(ctx context.Context, author, name string) (*Skill, error) {
	s, err := svc.queries.GetSkillByName(ctx, db.GetSkillByNameParams{
		Author: author,
		Name:   name,
	})
	if err != nil {
		return nil, fmt.Errorf("skill %q not found: %w", author+"/"+name, err)
	}
	return SkillFromDB(s), nil
}

func (svc *Service) ListSkills(ctx context.Context) ([]*Skill, error) {
	rows, err := svc.queries.ListSkills(ctx)
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	skills := make([]*Skill, len(rows))
	for i, s := range rows {
		skills[i] = SkillFromDB(s)
	}
	return skills, nil
}
