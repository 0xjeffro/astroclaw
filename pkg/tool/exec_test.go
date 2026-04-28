package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// makeExecArgs is a test helper that wraps a command string into the JSON
// format that OpenAI would send as tool_call arguments:
// {"command": "echo hello"}
func makeExecArgs(command string) string {
	b, _ := json.Marshal(map[string]string{"command": command})
	return string(b)
}

// TestExecCommand_BasicCommand verifies the happy path: a simple command
// runs successfully and its stdout is returned.
func TestExecCommand_BasicCommand(t *testing.T) {
	got, err := (&ExecCommandTool{}).Execute(context.Background(), makeExecArgs("echo hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(got) != "hello" {
		t.Errorf("got %q, want %q", got, "hello\n")
	}
}

// TestExecCommand_Pipe verifies that shell features like pipes work,
// confirming that "sh -c" delegation is functioning correctly. Without
// "sh -c", the pipe character would be passed as a literal argument
// and the command would fail.
func TestExecCommand_Pipe(t *testing.T) {
	got, err := (&ExecCommandTool{}).Execute(context.Background(), makeExecArgs("echo hello | tr h H"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(got) != "Hello" {
		t.Errorf("got %q, want %q", got, "Hello\n")
	}
}

// TestExecCommand_FailedCommand verifies that a non-zero exit code does
// NOT cause Run to return an error. Instead, the exit status is appended
// to the output as "[exit error: ...]". This is critical because command
// "failure" is often normal (e.g. go test with failing tests, grep with
// no matches) — the model needs to see the output to understand what
// went wrong, not just a generic error.
func TestExecCommand_FailedCommand(t *testing.T) {
	got, err := (&ExecCommandTool{}).Execute(context.Background(), makeExecArgs("echo 'some output' && false"))
	// err should be nil — command failure is reported in the result, not as an error
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "some output") {
		t.Errorf("result %q should contain the command output", got)
	}
	if !strings.Contains(got, "[exit error:") {
		t.Errorf("result %q should contain '[exit error:'", got)
	}
}

// TestExecCommand_Stderr verifies that stderr output is captured alongside
// stdout. CombinedOutput merges both streams, so diagnostic messages and
// error output are visible to the model.
func TestExecCommand_Stderr(t *testing.T) {
	got, err := (&ExecCommandTool{}).Execute(context.Background(), makeExecArgs("echo err_msg >&2"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "err_msg") {
		t.Errorf("result %q should contain stderr output 'err_msg'", got)
	}
}

// TestExecCommand_Truncation verifies that output exceeding 64KB is
// truncated with a [TRUNCATED] marker, preventing a single verbose command
// from blowing up the LLM context window.
func TestExecCommand_Truncation(t *testing.T) {
	// Generate 80KB of 'a' — well over the 64KB limit.
	got, err := (&ExecCommandTool{}).Execute(context.Background(), makeExecArgs("head -c 80000 /dev/zero | tr '\\0' 'a'"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(got, "[TRUNCATED]") {
		t.Error("expected result to end with [TRUNCATED]")
	}
}

// TestExecCommand_BadJSON verifies that malformed JSON arguments return
// an error rather than panicking.
func TestExecCommand_BadJSON(t *testing.T) {
	_, err := (&ExecCommandTool{}).Execute(context.Background(), "not json")
	if err == nil {
		t.Fatal("expected error for bad JSON, got nil")
	}
	if !strings.Contains(err.Error(), "bad args") {
		t.Errorf("error %q should contain 'bad args'", err.Error())
	}
}
