package tool

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type EditFileTool struct{}

func (t *EditFileTool) Name() string { return "edit_file" }
func (t *EditFileTool) Description() string {
	return "Edits a file by replacing an exact occurrence of old_text with new_text. " +
		"old_text must appear exactly once in the file. " +
		"Use empty new_text to delete text."
}
func (t *EditFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the file to edit.",
			},
			"old_text": map[string]any{
				"type":        "string",
				"description": "The exact text to find. Must appear exactly once in the file.",
			},
			"new_text": map[string]any{
				"type":        "string",
				"description": "The replacement text. Use empty string to delete old_text.",
			},
		},
		// `new_text` is required, this is because even if the LLM wants to delete text, they should explicitly set `new_text` to an empty string.
		// This avoids LLM from forgetting to pass `new_text` and causing accidental deletion.
		"required": []string{"path", "old_text", "new_text"},
	}
}
func (t *EditFileTool) Execute(args string) (string, error) {
	var p struct {
		Path    string `json:"path"`
		OldText string `json:"old_text"`
		NewText string `json:"new_text"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return "", fmt.Errorf("bad args: %w", err)
	}
	data, err := os.ReadFile(p.Path)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	content := string(data)
	if !strings.Contains(content, p.OldText) {
		return "", fmt.Errorf("old_text not found in file. Make sure it matches exactly")
	}
	count := strings.Count(content, p.OldText)
	if count > 1 {
		return "", fmt.Errorf("old_text appears %d times. Provide more context to make it unique", count)
	}
	newContent := strings.Replace(content, p.OldText, p.NewText, 1)
	if err := os.WriteFile(p.Path, []byte(newContent), 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return fmt.Sprintf("edited %s", p.Path), nil
}
func (t *EditFileTool) Approval() bool  { return true }
func (t *EditFileTool) Workspace() bool { return true }
