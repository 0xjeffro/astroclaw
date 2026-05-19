package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxOutputLines = 2000
	maxOutputBytes = 50 * 1024 // 50KB
)

// NOTE: if you change execTimeout, update the Description below to match.
const execTimeout = 30 * time.Second

type ExecCommandTool struct{}

func (t *ExecCommandTool) Name() string { return "exec_command" }
func (t *ExecCommandTool) Description() string {
	return "Executes a shell command and returns its combined stdout and stderr output. " +
		"Timeout is 30 seconds. Output is capped at 2000 lines or 50KB."
}
func (t *ExecCommandTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The command to execute.",
			},
		},
		"required": []string{"command"},
	}
}
func (t *ExecCommandTool) Execute(parentCtx context.Context, args string) *ToolResult {
	var p struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return &ToolResult{
			IsError: true,
			ForLLM:  fmt.Sprintf("bad args: %s", err),
			Err:     err,
		}
	}

	ctx, cancel := context.WithTimeout(parentCtx, execTimeout)
	defer cancel()

	// "sh -c" delegates the entire command string to a real shell, so pipes,
	// redirects, wildcards, $ENV, && chains etc. all work out of the box
	// without us having to parse or split the command ourselves.
	cmd := exec.CommandContext(ctx, "sh", "-c", p.Command)
	// CombinedOutput captures both stdout and stderr in one byte slice,
	// so the model sees the full picture.
	output, err := cmd.CombinedOutput()

	result, _ := truncateOutput(string(output))

	if err != nil {
		return &ToolResult{
			IsError: true,
			ForLLM:  fmt.Sprintf("%s\n[exit error: %v]", result, err),
			Err:     err,
		}
	}

	return &ToolResult{
		ForLLM: result,
	}
}
func (t *ExecCommandTool) Approval() bool  { return true }
func (t *ExecCommandTool) Workspace() bool { return true }

// truncateOutput limits output by both line count and byte size,
// whichever is hit first, inspired by OpenCode's design here.
// When truncating at the byte limit, it backs up to the nearest UTF-8
// character boundary to avoid splitting multi-byte characters.
func truncateOutput(s string) (string, bool) {
	lines := strings.SplitAfter(s, "\n")

	var b strings.Builder
	lineCount := 0
	for _, line := range lines {
		// Check line limit.
		if lineCount >= maxOutputLines {
			b.WriteString(fmt.Sprintf("\n[TRUNCATED: exceeded %d lines]", maxOutputLines))
			return b.String(), true
		}
		// Check byte limit. If adding this line would exceed the limit,
		// truncate the line at a UTF-8 boundary.
		if b.Len()+len(line) > maxOutputBytes {
			remaining := maxOutputBytes - b.Len()
			if remaining > 0 {
				truncated := truncateAtUTF8Boundary(line, remaining)
				b.WriteString(truncated)
			}
			b.WriteString(fmt.Sprintf("\n[TRUNCATED: exceeded %d bytes]", maxOutputBytes))
			return b.String(), true
		}
		b.WriteString(line)
		lineCount++
	}
	return b.String(), false
}

// truncateAtUTF8Boundary truncates a string to at most maxBytes,
// backing up to avoid splitting a multi-byte UTF-8 character.
func truncateAtUTF8Boundary(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	// Back up from maxBytes until we're at the start of a UTF-8 character.
	// UTF-8 continuation bytes have the pattern 10xxxxxx (0x80-0xBF).
	i := maxBytes
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return s[:i]
}
