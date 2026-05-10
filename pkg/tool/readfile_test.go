package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeReadFileArgs is a test helper that wraps a file path into the JSON
// format that OpenAI would send as tool_call arguments:
// {"path": "/tmp/foo.txt"}
func makeReadFileArgs(path string) string {
	b, _ := json.Marshal(map[string]string{"path": path})
	return string(b)
}

// TestReadFile_NormalText verifies the happy path: a small text file is
// read in full, with contents returned exactly as written.
func TestReadFile_NormalText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hello.txt")
	_ = os.WriteFile(path, []byte("hello world"), 0644)

	got, err := (&ReadFileTool{}).Execute(context.Background(), makeReadFileArgs(path))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello world" {
		t.Errorf("got %q, want %q", got, "hello world")
	}
}

// TestReadFile_EmptyFile verifies that an empty file returns an empty
// string without error. This is a valid case — the model should see ""
// and can tell the user "the file is empty".
func TestReadFile_EmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.txt")
	_ = os.WriteFile(path, []byte{}, 0644)

	got, err := (&ReadFileTool{}).Execute(context.Background(), makeReadFileArgs(path))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

// TestReadFile_NotFound verifies that a non-existent file returns an error
// containing the file path, so the model can tell the user which file was
// not found.
func TestReadFile_NotFound(t *testing.T) {
	_, err := (&ReadFileTool{}).Execute(context.Background(), makeReadFileArgs("/no/such/file.txt"))
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}
	if !strings.Contains(err.Error(), "/no/such/file.txt") {
		t.Errorf("error %q should contain the file path", err.Error())
	}
}

// TestReadFile_Truncation verifies that files exceeding 2000 lines are
// truncated and the result ends with a [TRUNCATED] marker. This prevents
// a single large file from blowing up the LLM context window. The model
// sees [TRUNCATED] and can tell the user only a partial read was done.
func TestReadFile_Truncation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.txt")
	// Write 2500 lines, well over the 2000 line limit.
	var content strings.Builder
	for i := range 2500 {
		_, _ = fmt.Fprintf(&content, "line %d\n", i+1)
	}
	_ = os.WriteFile(path, []byte(content.String()), 0644)

	got, err := (&ReadFileTool{}).Execute(context.Background(), makeReadFileArgs(path))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "[TRUNCATED") {
		t.Error("expected result to contain [TRUNCATED]")
	}
	// Verify the first and last expected lines are present.
	if !strings.Contains(got, "line 1\n") {
		t.Error("expected first line to be present")
	}
	if !strings.Contains(got, "line 2000\n") {
		t.Error("expected line 2000 to be present")
	}
	if strings.Contains(got, "line 2001\n") {
		t.Error("line 2001 should not be present")
	}
}

// Verify that a file with few lines but exceeding 50KB is truncated at
// the byte limit, not the line limit.
func TestReadFile_TruncationByBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wide.txt")
	// Write 100 lines of 1000 chars each = 100KB, well over the 50KB limit
	// but only 100 lines (under the 2000 line limit).
	// Each line is under maxLineRunes (2000) so per-line truncation won't kick in.
	var content strings.Builder
	for i := range 100 {
		_, _ = fmt.Fprintf(&content, "line%d:", i+1)
		content.WriteString(strings.Repeat("x", 1000))
		content.WriteByte('\n')
	}
	_ = os.WriteFile(path, []byte(content.String()), 0644)

	got, err := (&ReadFileTool{}).Execute(context.Background(), makeReadFileArgs(path))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "[TRUNCATED") {
		t.Error("expected result to contain truncation marker")
	}
	if !strings.Contains(got, "bytes") {
		t.Error("expected truncation marker to mention bytes")
	}
}

// Verify that individual lines exceeding 2000 characters are truncated
// with a suffix, while the rest of the file is returned normally.
func TestReadFile_LongLineTruncation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "longline.txt")
	longLine := strings.Repeat("a", 3000)
	content := "short line\n" + longLine + "\nanother short line\n"
	_ = os.WriteFile(path, []byte(content), 0644)

	got, err := (&ReadFileTool{}).Execute(context.Background(), makeReadFileArgs(path))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, maxLineSuffix) {
		t.Error("expected long line to be truncated with suffix")
	}
	if !strings.Contains(got, "short line") {
		t.Error("short lines should be preserved")
	}
	if !strings.Contains(got, "another short line") {
		t.Error("lines after truncated line should be preserved")
	}
	// The truncated line should be exactly maxLineRunes runes + suffix.
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, maxLineSuffix) {
			runes := []rune(strings.TrimSuffix(line, maxLineSuffix))
			if len(runes) != maxLineRunes {
				t.Errorf("truncated line has %d runes, want %d", len(runes), maxLineRunes)
			}
		}
	}
}

// TestReadFile_BinaryFile verifies that files containing null bytes are
// rejected with an error. Binary files (executables, images, etc.) would
// produce garbage text and waste context tokens, so we refuse them early.
func TestReadFile_BinaryFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "binary.bin")
	// Write some bytes with a null byte in the middle.
	_ = os.WriteFile(path, []byte("hello\x00world"), 0644)

	_, err := (&ReadFileTool{}).Execute(context.Background(), makeReadFileArgs(path))
	if err == nil {
		t.Fatal("expected error for binary file, got nil")
	}
	if !strings.Contains(err.Error(), "binary") {
		t.Errorf("error %q should mention 'binary'", err.Error())
	}
}

// TestReadFile_BadJSON verifies that malformed JSON arguments return an
// error rather than panicking.
func TestReadFile_BadJSON(t *testing.T) {
	_, err := (&ReadFileTool{}).Execute(context.Background(), "not json")
	if err == nil {
		t.Fatal("expected error for bad JSON, got nil")
	}
	if !strings.Contains(err.Error(), "bad args") {
		t.Errorf("error %q should contain 'bad args'", err.Error())
	}
}
