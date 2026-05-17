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

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jackc/pgx/v5/pgxpool"
)

// seedDefaultSkills reads skill directories bundled with the Lambda,
// uploads each file to S3, and writes metadata to DSQL.
// Skips skills that already exist in the database.
func seedDefaultSkills(ctx context.Context, pool *pgxpool.Pool) error {
	bucket := os.Getenv("SKILLS_BUCKET")
	if bucket == "" {
		log.Println("SKILLS_BUCKET not set, skipping skill seeding")
		return nil
	}

	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}
	s3Client := s3.NewFromConfig(awsCfg)

	skillsSvc := skills.NewService(pool)

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

			// Only skip if the skill definitively exists (err == nil and record returned).
			// If err != nil it could be a DB connection issue, not "not found".
			// In that case we proceed and let CreateSkill's UNIQUE constraint handle it.
			existing, err := skillsSvc.GetSkillByName(ctx, author, skillName)
			if err == nil && existing != nil {
				log.Printf("skill %s/%s already exists, skipping", author, skillName)
				continue
			}
			// If we reach here: err != nil (possible DB issue) or existing == nil (skill really doesn't exist).
			// Either way, proceed with seeding.

			// Read SKILL.md to extract description from frontmatter.
			content, err := os.ReadFile(skillMD)
			if err != nil {
				log.Printf("read %s: %v", skillMD, err)
				continue
			}
			description, whenToUse := parseFrontmatter(string(content))

			// Upload S3 first, then write DB. DB is the source of truth for the agent at runtime:
			// if a skill has a DB record, the agent expects its
			// files to fully exist in S3. This ordering ensures:
			//
			// Scenario A: S3 upload succeeds, DB write fails
			// Skill is invisible to agent. Next deploy retries and succeeds (S3 PutObject is idempotent).
			//
			// Scenario B: S3 partially uploads, then fails
			// Same as A. Skill invisible, next deploy overwrites all files.
			if err := uploadSkillFiles(ctx, s3Client, bucket, author, skillName, skillDir); err != nil {
				log.Printf("upload %s/%s to S3: %v", author, skillName, err)
				continue
			}

			// Write metadata to DSQL.
			_, err = skillsSvc.CreateSkill(ctx, author, skillName, description, whenToUse, nil, "0.1.0")
			if err != nil {
				log.Printf("create skill %s/%s in DB: %v", author, skillName, err)
				continue
			}

			log.Printf("seeded skill: %s/%s", author, skillName)
		}
	}

	return nil
}

// uploadSkillFiles uploads all files in a skill directory to S3,
// preserving the directory structure under skills/{author}/{name}/.
func uploadSkillFiles(ctx context.Context, client *s3.Client, bucket, author, name, dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		// Build S3 key: skills/@author/name/relative-path
		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		s3Key := fmt.Sprintf("skills/%s/%s/%s", author, name, relPath)

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

// parseFrontmatter extracts description and when_to_use from SKILL.md YAML frontmatter.
func parseFrontmatter(content string) (description, whenToUse string) {
	// TODO: when Skill Store is implemented, valid frontmatter should
	// be a hard requirement for pre-validating. Reject skills without it.
	parts := strings.SplitN(content, "---", 3)
	if len(parts) < 3 {
		log.Printf("warning: SKILL.md has no valid YAML frontmatter (missing --- delimiters)")
		return "", ""
	}
	frontmatter := parts[1]
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "description:") {
			description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			description = strings.Trim(description, "\"'")
		}
		if strings.HasPrefix(line, "when_to_use:") {
			whenToUse = strings.TrimSpace(strings.TrimPrefix(line, "when_to_use:"))
			whenToUse = strings.Trim(whenToUse, "\"'")
		}
	}
	return
}
