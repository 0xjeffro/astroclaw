package skills

import (
	"fmt"
	"strings"
	"time"

	"astroclaw/pkg/app/skills/db"
)

type Skill struct {
	ID          string
	Author      string
	Name        string
	Description string
	WhenToUse   string
	Tags        []string
	Version     string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// FullName returns the "author/name" identifier for this skill.
func (s *Skill) FullName() string {
	if s.Author == "" {
		return s.Name
	}
	return s.Author + "/" + s.Name
}

// S3Key returns the S3 object key for this skill's ZIP archive.
func (s *Skill) S3Key() string {
	return fmt.Sprintf("skills/%s/%s.zip", s.Author, s.Name)
}

func SkillFromDB(s db.AppSkill) *Skill {
	var tags []string
	if s.Tags != "" {
		tags = strings.Split(s.Tags, ",")
	}
	return &Skill{
		ID:          s.ID,
		Author:      s.Author,
		Name:        s.Name,
		Description: s.Description,
		WhenToUse:   s.WhenToUse,
		Tags:        tags,
		Version:     s.Version,
		CreatedAt:   s.CreatedAt,
		UpdatedAt:   s.UpdatedAt,
	}
}

// FormatForPrompt builds a compact skill index for the system prompt.
// Only includes name, description, and when_to_use to conserve tokens.
func FormatForPrompt(skills []*Skill) string {
	if len(skills) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Available skills (use load_skill tool to load full content):\n")
	for _, s := range skills {
		b.WriteString("- " + s.FullName() + ": " + s.Description)
		if s.WhenToUse != "" {
			b.WriteString(" | When to use: " + s.WhenToUse)
		}
		b.WriteByte('\n')
	}
	return b.String()
}
