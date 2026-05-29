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

// InstallSkill records a skill installation in the given workspace.
// The metadata is snapshotted at install time so the agent doesn't need
// to call the remote skill store on every prompt build.
func (svc *Service) InstallSkill(ctx context.Context, workspaceID, author, name, version, description, whenToUse string, tags []string) (*Skill, error) {
	s, err := svc.queries.InstallSkill(ctx, db.InstallSkillParams{
		WorkspaceID: workspaceID,
		Author:      author,
		Name:        name,
		Version:     version,
		Description: description,
		WhenToUse:   whenToUse,
		Tags:        strings.Join(tags, ","),
	})
	if err != nil {
		return nil, fmt.Errorf("install skill: %w", err)
	}
	return SkillFromDB(s), nil
}

// GetSkillFromWorkspace returns the (author, name) skill installed in the
// given workspace, or an error if not installed.
func (svc *Service) GetSkillFromWorkspace(ctx context.Context, workspaceID, author, name string) (*Skill, error) {
	s, err := svc.queries.GetSkillFromWorkspace(ctx, db.GetSkillFromWorkspaceParams{
		WorkspaceID: workspaceID,
		Author:      author,
		Name:        name,
	})
	if err != nil {
		return nil, fmt.Errorf("skill %q not installed in workspace: %w", author+"/"+name, err)
	}
	return SkillFromDB(s), nil
}

// ListSkillsByWorkspace returns all skills installed in the given workspace.
func (svc *Service) ListSkillsByWorkspace(ctx context.Context, workspaceID string) ([]*Skill, error) {
	rows, err := svc.queries.ListSkillsByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	skills := make([]*Skill, len(rows))
	for i, s := range rows {
		skills[i] = SkillFromDB(s)
	}
	return skills, nil
}

// UpdateSkillInWorkspace UpdateSkill changes / refreshed metadata (like, version) of an
// already-installed skill in the given workspace. Used for upgrade/downgrade.
func (svc *Service) UpdateSkillInWorkspace(ctx context.Context, workspaceID, author, name, version, description, whenToUse string, tags []string) error {
	return svc.queries.UpdateSkillInWorkspace(ctx, db.UpdateSkillInWorkspaceParams{
		WorkspaceID: workspaceID,
		Author:      author,
		Name:        name,
		Version:     version,
		Description: description,
		WhenToUse:   whenToUse,
		Tags:        strings.Join(tags, ","),
	})
}

// UninstallSkillInWorkspace UninstallSkill removes a skill from the workspace's installed set.
// The S3 content is not deleted (it may be in use by other workspaces).
func (svc *Service) UninstallSkillInWorkspace(ctx context.Context, workspaceID, author, name string) error {
	return svc.queries.UninstallSkillInWorkspace(ctx, db.UninstallSkillInWorkspaceParams{
		WorkspaceID: workspaceID,
		Author:      author,
		Name:        name,
	})
}
