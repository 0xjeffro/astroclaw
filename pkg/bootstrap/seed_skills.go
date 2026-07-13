package bootstrap

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"astroclaw/pkg/app/skills"
	"astroclaw/pkg/app/system"

	"gocloud.dev/blob"
)

// seedDefaultSkills scans skillsDir for @author/name/SKILL.md, uploads
// the files to the bucket under skills/{author}/{name}/{version}/...,
// and writes an install record per workspace. Skips skills that are
// already installed in the workspace.
//
// Storage-first, DB-second: DB is the source of truth at runtime, so
// a half-uploaded skill without a DB record is invisible and safe.
// WriteAll is idempotent, so retrying overwrites cleanly.
func seedDefaultSkills(ctx context.Context, sysSvc *system.Service, skillsSvc *skills.Service, bucket *blob.Bucket, skillsDir string) error {
	admin, err := sysSvc.GetAdmin(ctx)
	if err != nil {
		return fmt.Errorf("get admin for skill seed: %w", err)
	}
	workspaces, err := sysSvc.ListWorkspacesForUser(ctx, admin.ID)
	if err != nil {
		return fmt.Errorf("list admin workspaces: %w", err)
	}
	if len(workspaces) == 0 {
		return fmt.Errorf("admin has no workspace to seed skills into")
	}
	workspaceID := workspaces[0].ID

	authors, err := filepath.Glob(filepath.Join(skillsDir, "@*"))
	if err != nil {
		return fmt.Errorf("glob skill authors: %w", err)
	}

	for _, authorDir := range authors {
		author := filepath.Base(authorDir)

		skillDirs, err := filepath.Glob(filepath.Join(authorDir, "*"))
		if err != nil {
			continue
		}

		for _, skillDir := range skillDirs {
			skillName := filepath.Base(skillDir)
			skillMD := filepath.Join(skillDir, "SKILL.md")

			if _, err := os.Stat(skillMD); err != nil {
				log.Printf("warning: %s/%s has no SKILL.md, skipping", author, skillName)
				continue
			}

			if existing, err := skillsSvc.GetSkillFromWorkspace(ctx, workspaceID, author, skillName); err == nil && existing != nil {
				log.Printf("skill %s/%s already installed in workspace, skipping", author, skillName)
				continue
			}

			content, err := os.ReadFile(skillMD)
			if err != nil {
				log.Printf("read %s: %v", skillMD, err)
				continue
			}
			description, whenToUse, version := parseFrontmatter(string(content))

			if err := uploadSkillFiles(ctx, bucket, author, skillName, version, skillDir); err != nil {
				log.Printf("upload %s/%s to storage: %v", author, skillName, err)
				continue
			}

			_, err = skillsSvc.InstallSkill(ctx, workspaceID, author, skillName, version, description, whenToUse, nil)
			if err != nil {
				log.Printf("install skill %s/%s in DB: %v", author, skillName, err)
				continue
			}

			log.Printf("seeded skill: %s/%s@%s", author, skillName, version)
		}
	}
	return nil
}

// uploadSkillFiles walks dir and uploads every regular file under
// skills/{author}/{name}/{version}/<relpath>.
func uploadSkillFiles(ctx context.Context, bucket *blob.Bucket, author, name, version, dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		key := fmt.Sprintf("skills/%s/%s/%s/%s", author, name, version, relPath)

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if err := bucket.WriteAll(ctx, key, data, nil); err != nil {
			return fmt.Errorf("put %s: %w", key, err)
		}
		log.Printf("  uploaded: %s", key)
		return nil
	})
}

// parseFrontmatter extracts description, when_to_use, and version from
// SKILL.md YAML frontmatter. Missing fields return empty strings.
func parseFrontmatter(content string) (description, whenToUse, version string) {
	// TODO: when Skill Store lands, valid frontmatter should be a hard
	// requirement. For now we silently accept missing fields.
	parts := strings.SplitN(content, "---", 3)
	if len(parts) < 3 {
		log.Printf("warning: SKILL.md has no valid YAML frontmatter (missing --- delimiters)")
		return "", "", ""
	}
	frontmatter := parts[1]
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "description:"):
			description = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "description:")), "\"'")
		case strings.HasPrefix(line, "when_to_use:"):
			whenToUse = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "when_to_use:")), "\"'")
		case strings.HasPrefix(line, "version:"):
			version = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "version:")), "\"'")
		}
	}
	return
}
