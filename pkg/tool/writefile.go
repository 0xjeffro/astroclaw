package tool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type WriteFileTool struct{}

func (t *WriteFileTool) Name() string { return "write_file" }
func (t *WriteFileTool) Description() string {
	return "Writes content to a file. If the file already exists, overwrite must be set to true. " +
		"Parent directories are created automatically."
}
func (t *WriteFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Absolute or relative path to the file.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "The content to write to the file.",
			},
			"overwrite": map[string]any{
				"type":        "boolean",
				"description": "Must be true to overwrite an existing file. Defaults to false.",
			},
		},
		"required": []string{"path", "content"},
	}
}
func (t *WriteFileTool) Execute(args string) (string, error) {
	var p struct {
		Path      string `json:"path"`
		Content   string `json:"content"`
		Overwrite bool   `json:"overwrite"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return "", fmt.Errorf("bad args: %w", err)
	}
	if !p.Overwrite {
		if _, err := os.Stat(p.Path); err == nil {
			return "", fmt.Errorf("file %s already exists, set overwrite=true to replace", p.Path)
		}
	}
	if dir := filepath.Dir(p.Path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create directory: %w", err)
		}
	}
	if err := os.WriteFile(p.Path, []byte(p.Content), 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(p.Content), p.Path), nil
}
func (t *WriteFileTool) Approval() bool  { return true }
func (t *WriteFileTool) Workspace() bool { return true }
