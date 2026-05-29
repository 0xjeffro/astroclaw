package skills

import (
	"strings"
	"time"

	"astroclaw/pkg/app/skills/db"
)

type Skill struct {
	WorkspaceID string
	Author      string
	Name        string
	Version     string
	Description string
	WhenToUse   string
	Tags        []string
	InstalledAt time.Time
	UpdatedAt   time.Time
}

// FullName returns the "author/name" identifier for this skill.
func (s *Skill) FullName() string {
	if s.Author == "" {
		return s.Name
	}
	return s.Author + "/" + s.Name
}

func SkillFromDB(s db.AppSkill) *Skill {
	var tags []string
	if s.Tags != "" {
		tags = strings.Split(s.Tags, ",")
	}
	return &Skill{
		WorkspaceID: s.WorkspaceID,
		Author:      s.Author,
		Name:        s.Name,
		Version:     s.Version,
		Description: s.Description,
		WhenToUse:   s.WhenToUse,
		Tags:        tags,
		InstalledAt: s.InstalledAt,
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
