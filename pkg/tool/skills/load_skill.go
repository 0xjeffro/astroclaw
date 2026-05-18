package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	appskills "astroclaw/pkg/app/skills"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Tool implements the load_skill tool that agents use to load skill content.
type Tool struct {
	Skills   *appskills.Service
	S3Client *s3.Client
	Bucket   string
}

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

	// Verify skill exists in DB.
	skill, err := t.Skills.GetSkillByName(ctx, author, name)
	if err != nil {
		return fmt.Sprintf("error: skill %s/%s not found", author, name), nil
	}

	// If a specific file is requested, read just that file.
	file := strings.TrimSpace(parsed.File)
	if file != "" {
		content, err := t.readS3File(ctx, author, name, file)
		if err != nil {
			return fmt.Sprintf("error: could not read %s: %v", file, err), nil
		}
		return content, nil
	}

	// Default: return SKILL.md content + file listing.
	skillMD, err := t.readS3File(ctx, author, name, "SKILL.md")
	if err != nil {
		return fmt.Sprintf("error: could not read SKILL.md: %v", err), nil
	}

	// List all files in the skill directory.
	files, err := t.listS3Files(ctx, author, name)
	if err != nil {
		return fmt.Sprintf("error: could not list files: %v", err), nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("[Skill: %s/%s]\n\n", skill.Author, skill.Name))
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

// readS3File reads a single file from the skill's S3 directory.
// TODO: abstract S3 calls behind a storage interface for multi-cloud support.
// TODO: add a size limit (io.LimitReader) so a huge file doesn't eat all Lambda memory.
// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/s3#Client.GetObject
func (t *Tool) readS3File(ctx context.Context, author, name, file string) (string, error) {
	resp, err := t.S3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &t.Bucket,
		Key:    new(fmt.Sprintf("skills/%s/%s/%s", author, name, file)),
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// listS3Files lists all files under a skill's S3 prefix.
// TODO: this only returns the first 1000 files. Fine for skills (they're small),
// but should use a paginator if skills ever get that big.
// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/s3#Client.ListObjectsV2
func (t *Tool) listS3Files(ctx context.Context, author, name string) ([]string, error) {
	prefix := fmt.Sprintf("skills/%s/%s/", author, name)
	resp, err := t.S3Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: &t.Bucket,
		Prefix: &prefix,
	})
	if err != nil {
		return nil, err
	}

	var files []string
	for _, obj := range resp.Contents {
		// Strip the prefix to get relative paths.
		relPath := strings.TrimPrefix(*obj.Key, prefix)
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
