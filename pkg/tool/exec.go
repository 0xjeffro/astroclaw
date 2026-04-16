package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

const maxOutputBytes = 64 * 1024 // 64KB

// NOTE: if you change execTimeout, update the Description below to match.
const execTimeout = 30 * time.Second

var ExecCommand = Tool{
	Name: "exec_command",
	Description: "Executes a shell command and returns its combined stdout and stderr output. " +
		"Timeout is 30 seconds. Output is capped at 64KB.",
	NeedsApproval: true,
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The command to execute.",
			},
		},
		"required": []string{"command"},
	},
	Run: func(args string) (string, error) {
		var p struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal([]byte(args), &p); err != nil {
			return "", fmt.Errorf("bad args: %w", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
		defer cancel()

		// "sh -c" delegates the entire command string to a real shell, so pipes,
		// redirects, wildcards, $ENV, && chains etc. all work out of the box
		// without us having to parse or split the command ourselves.
		cmd := exec.CommandContext(ctx, "sh", "-c", p.Command)
		// CombinedOutput captures both stdout and stderr in one byte slice,
		// so the model sees the full picture — normal output and errors together.
		output, err := cmd.CombinedOutput()

		truncated := false
		if len(output) > maxOutputBytes {
			output = output[:maxOutputBytes]
			truncated = true
		}

		result := string(output)
		if truncated {
			result += "\n[TRUNCATED]"
		}

		// When the command fails, return the exit code and output together as the result
		// This way, the model can see the error output and decide how to handle it itself
		if err != nil {
			return fmt.Sprintf("%s\n[exit error: %v]", result, err), nil
		}

		return result, nil
	},
}
