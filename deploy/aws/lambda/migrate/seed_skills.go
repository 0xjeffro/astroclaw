package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"astroclaw/pkg/app/skills"
	"astroclaw/pkg/app/system"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// seedDefaultSkills reads skill directories bundled with the Lambda,
// uploads each file to S3 under skills/{author}/{name}/{version}/...,
// and writes a per-workspace installation record to DSQL.
// Skips skills that already exist in the database.
func seedDefaultSkills(ctx context.Context, sysSvc *system.Service, skillsSvc *skills.Service, s3Client *s3.Client) error {
	bucket := os.Getenv("SKILLS_BUCKET")
	if bucket == "" {
		log.Println("SKILLS_BUCKET not set, skipping skill seeding")
		return nil
	}

	// Find the default workspace (the admin's first workspace).
	admin, err := sysSvc.GetAdmin(ctx)
	if err != nil {
		return fmt.Errorf("get admin for agent seed: %w", err)
	}
	workspaces, err := sysSvc.ListWorkspacesForUser(ctx, admin.ID)
	if err != nil {
		return fmt.Errorf("list admin workspaces: %w", err)
	}
	if len(workspaces) == 0 {
		return fmt.Errorf("admin has no workspace to seed agent into")
	}
	workspaceID := workspaces[0].ID

	// Scan skills/ directory for @author/name directories containing SKILL.md.
	authors, err := filepath.Glob("skills/@*")
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

			// Only skip if the skill definitively exists for this workspace
			// (err == nil and record returned). If err != nil it could be a
			// DB connection issue, not "not found"; let the InstallSkill call below surface the real error.
			if existing, err := skillsSvc.GetSkillFromWorkspace(ctx, workspaceID, author, skillName); err == nil && existing != nil {
				log.Printf("skill %s/%s already installed in workspace, skipping", author, skillName)
				continue
			}

			// Read SKILL.md to extract description from frontmatter.
			content, err := os.ReadFile(skillMD)
			if err != nil {
				log.Printf("read %s: %v", skillMD, err)
				continue
			}
			description, whenToUse, version := parseFrontmatter(string(content))

			// Upload S3 first, then write DB. DB is the source of truth for the agent at runtime:
			// if a skill has a DB record, the agent expects its
			// files to fully exist in S3. This ordering ensures:
			//
			// Scenario A: S3 upload succeeds, DB write fails
			// Skill is invisible to agent. Next deploy retries and succeeds (S3 PutObject is idempotent).
			//
			// Scenario B: S3 partially uploads, then fails
			// Same as A. Skill invisible, next deploy overwrites all files.
			if err := uploadSkillFiles(ctx, s3Client, bucket, author, skillName, version, skillDir); err != nil {
				log.Printf("upload %s/%s to S3: %v", author, skillName, err)
				continue
			}

			// Write metadata to DSQL.
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

// uploadSkillFiles uploads all files in a skill directory to S3,
// preserving the directory structure under skills/{author}/{name}/{version}/.
func uploadSkillFiles(ctx context.Context, client *s3.Client, bucket, author, name, version, dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		// Build S3 key: skills/@author/name/version/relative-path
		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		s3Key := fmt.Sprintf("skills/%s/%s/%s/%s", author, name, version, relPath)

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}

		_, err = client.PutObject(ctx, &s3.PutObjectInput{
			Bucket: &bucket,
			Key:    &s3Key,
			Body:   bytes.NewReader(data),
		})
		if err != nil {
			return fmt.Errorf("put %s: %w", s3Key, err)
		}

		log.Printf("  uploaded: %s", s3Key)
		return nil
	})
}

// parseFrontmatter extracts description, when_to_use, and version from
// SKILL.md YAML frontmatter. Missing fields are returned as empty strings.
func parseFrontmatter(content string) (description, whenToUse, version string) {
	// TODO: when Skill Store is implemented, valid frontmatter should
	// be a hard requirement for pre-validating. Reject skills without it.
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
