package tool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

const maxReadBytes = 64 * 1024 // 64KB

type ReadFileTool struct{}

func (t *ReadFileTool) Name() string { return "read_file" }
func (t *ReadFileTool) Description() string {
	return "Reads the contents of a text file. Returns up to 64KB. Binary files are rejected."
}
func (t *ReadFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Absolute or relative path to the text file.",
			},
		},
		"required": []string{"path"},
	}
}
func (t *ReadFileTool) Execute(args string) (string, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return "", fmt.Errorf("bad args: %w", err)
	}
	data, err := os.ReadFile(p.Path)
	if err != nil {
		return "", fmt.Errorf("read file %s: %w", p.Path, err)
	}
	if bytes.Contains(data, []byte{0}) {
		return "", fmt.Errorf("binary file, cannot read as text")
	}
	if len(data) > maxReadBytes {
		return string(data[:maxReadBytes]) + "\n[TRUNCATED]", nil
	}
	return string(data), nil
}
func (t *ReadFileTool) Approval() bool  { return false }
func (t *ReadFileTool) Workspace() bool { return true }
