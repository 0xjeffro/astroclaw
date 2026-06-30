package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	appskills "astroclaw/pkg/app/skills"

	"gocloud.dev/blob"
)

// Tool implements the load_skill tool that agents use to load skill content.
type Tool struct {
	Skills      *appskills.Service
	Bucket      *blob.Bucket
	WorkspaceID string
}

// TODO: Go's named struct literals silently zero-initialize omitted fields,
// so a required field like WorkspaceID can be forgotten at construction
// without any build error. For example, when reply Lambda's createFn builds
// this struct, leaving out WorkspaceID compiles fine but breaks every
// load_skill call at runtime ("skill not found").
// Plan: replace exported fields with a New(...) constructor that forces all
// required fields to be passed explicitly, and unexport the struct fields.

func (t *Tool) Name() string { return "load_skill" }

func (t *Tool) Description() string {
	return "Load a skill's content from storage. " +
		"Returns the SKILL.md content and a list of supporting files. " +
		"Use the file parameter to read a specific supporting file."
}

func (t *Tool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"author": map[string]any{
				"type":        "string",
				"description": "Skill author, start with @ (e.g. @astroclaw, @username)",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Skill name",
			},
			"file": map[string]any{
				"type":        "string",
				"description": "Optional: specific file to read (e.g. references/api.md). If omitted, returns SKILL.md and file listing.",
			},
		},
		"required": []string{"author", "name"},
	}
}

func (t *Tool) Approval() bool  { return false }
func (t *Tool) Workspace() bool { return false }

func (t *Tool) Execute(ctx context.Context, args string) (string, error) {
	var parsed struct {
		Author string `json:"author"`
		Name   string `json:"name"`
		File   string `json:"file"`
	}
	if err := json.Unmarshal([]byte(args), &parsed); err != nil {
		return fmt.Sprintf("error: invalid arguments: %v", err), nil
	}

	author := strings.TrimSpace(parsed.Author)
	name := strings.TrimSpace(parsed.Name)
	if author == "" {
		return "error: author is required", nil
	}
	if name == "" {
		return "error: name is required", nil
	}

	// Verify skill exists in this workspace.
	skill, err := t.Skills.GetSkillFromWorkspace(ctx, t.WorkspaceID, author, name)
	if err != nil {
		return fmt.Sprintf("error: skill %s/%s not found in workspace %s", author, name, t.WorkspaceID), nil
	}

	// If a specific file is requested, read just that file.
	file := strings.TrimSpace(parsed.File)
	if file != "" {
		content, err := t.readFile(ctx, author, name, skill.Version, file)
		if err != nil {
			return fmt.Sprintf("error: could not read %s: %v", file, err), nil
		}
		return content, nil
	}

	// Default: return SKILL.md content + file listing.
	skillMD, err := t.readFile(ctx, author, name, skill.Version, "SKILL.md")
	if err != nil {
		return fmt.Sprintf("error: could not read SKILL.md: %v", err), nil
	}

	// List all files in the skill directory.
	files, err := t.listFiles(ctx, author, name, skill.Version)
	if err != nil {
		return fmt.Sprintf("error: could not list files: %v", err), nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("[Skill: %s/%s Version: %s]\n\n", skill.Author, skill.Name, skill.Version))
	b.WriteString(skillMD)

	// Show supporting files if any exist beyond SKILL.md.
	supporting := filterSupportingFiles(files)
	if len(supporting) > 0 {
		b.WriteString("\n\n[This skill has supporting files:]\n")
		for _, f := range supporting {
			b.WriteString(fmt.Sprintf("- %s\n", f))
		}
		b.WriteString(fmt.Sprintf("\nLoad any of these with load_skill(author=\"%s\", name=\"%s\", file=\"<path>\").\n", author, name))
	}

	return b.String(), nil
}

// readFile reads a single file from the skill's storage directory.
// TODO: add a size limit (io.LimitReader) so a huge file doesn't eat all Lambda memory.
func (t *Tool) readFile(ctx context.Context, author, name, version, file string) (string, error) {
	key := fmt.Sprintf("skills/%s/%s/%s/%s", author, name, version, file)
	data, err := t.Bucket.ReadAll(ctx, key)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// listFiles lists all files under a skill's storage prefix.
// TODO: this only returns the first page. Fine for skills (they're small),
// but should iterate fully if a skill ever ships with more than a page of files.
func (t *Tool) listFiles(ctx context.Context, author, name, version string) ([]string, error) {
	prefix := fmt.Sprintf("skills/%s/%s/%s/", author, name, version)
	iter := t.Bucket.List(&blob.ListOptions{Prefix: prefix})
	var files []string
	for {
		obj, err := iter.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		relPath := strings.TrimPrefix(obj.Key, prefix)
		if relPath != "" {
			files = append(files, relPath)
		}
	}
	return files, nil
}

// filterSupportingFiles removes SKILL.md from the file list,
// returning only supporting files (references, templates, scripts, etc.)
func filterSupportingFiles(files []string) []string {
	var supporting []string
	for _, f := range files {
		if f != "SKILL.md" {
			supporting = append(supporting, f)
		}
	}
	return supporting
}
