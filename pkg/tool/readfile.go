package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

const (
	maxReadLines  = 2000      // max lines to return
	maxReadBytes  = 50 * 1024 // 50KB max output size
	maxLineRunes  = 2000      // truncate individual lines beyond this
	maxLineSuffix = "... (line truncated)"

	// TODO: add offset parameter so the model can paginate through large files
	// (e.g. read_file(path, offset=2001) to continue after truncation).
	// When offset is added, also add line numbers to output (e.g.
	// "1  package main", "2  import fmt") and include total line count in
	// truncation messages so the model knows where it is and how much is left.
	//
	// Per-line truncation (maxLineChars) is intentionally not recoverable.
	// Lines that long are usually not meant to be read directly (minified JS,
	// base64, giant JSON single-lines). The model can use exec_command with
	// cut/python/jq to extract specific parts if needed.
)

type ReadFileTool struct{}

func (t *ReadFileTool) Name() string { return "read_file" }
func (t *ReadFileTool) Description() string {
	return "Reads the contents of a text file. Returns up to 2000 lines or 50KB. Binary files are rejected."
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
func (t *ReadFileTool) Execute(_ context.Context, args string) *ToolResult {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return &ToolResult{
			IsError: true,
			ForLLM:  fmt.Sprintf("bad args: %s", err),
			Err:     err,
		}
	}
	data, err := os.ReadFile(p.Path)
	if err != nil {
		return &ToolResult{
			IsError: true,
			ForLLM:  fmt.Sprintf("read file %s: %s", p.Path, err),
			Err:     err,
		}
	}
	if bytes.Contains(data, []byte{0}) {
		return &ToolResult{
			IsError: true,
			ForLLM:  "binary file, cannot read as text",
			Err:     fmt.Errorf("binary file, cannot read as text"),
		}
	}
	text := string(data)
	lines := strings.Split(text, "\n")

	var b strings.Builder
	lineCount := 0
	for _, line := range lines {
		// Truncate individual long lines.
		runes := []rune(line)
		if len(runes) > maxLineRunes {
			line = string(runes[:maxLineRunes]) + maxLineSuffix
		}

		// Check line limit.
		if lineCount >= maxReadLines {
			b.WriteString(fmt.Sprintf("\n[TRUNCATED: exceeded %d lines]", maxReadLines))
			return &ToolResult{ForLLM: b.String()}
		}

		// Check byte limit. Truncate at UTF-8 boundary if mid-line.
		entry := line + "\n"
		if b.Len()+len(entry) > maxReadBytes {
			remaining := maxReadBytes - b.Len()
			if remaining > 0 {
				b.WriteString(truncateAtUTF8(line, remaining))
			}
			b.WriteString(fmt.Sprintf("\n[TRUNCATED: exceeded %d bytes]", maxReadBytes))
			return &ToolResult{ForLLM: b.String()}
		}

		b.WriteString(entry)
		lineCount++
	}
	return &ToolResult{ForLLM: strings.TrimSuffix(b.String(), "\n")}
}
func (t *ReadFileTool) Approval() bool  { return false }
func (t *ReadFileTool) Workspace() bool { return true }

// truncateAtUTF8 truncates a string to at most maxBytes,
// backing up to avoid splitting a multi-byte UTF-8 character.
func truncateAtUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	i := maxBytes
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return s[:i]
}
