package tool

import (
	"bytes"
	"encoding/json"
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

	got, err := ReadFile.Run(makeReadFileArgs(path))
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

	got, err := ReadFile.Run(makeReadFileArgs(path))
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
	_, err := ReadFile.Run(makeReadFileArgs("/no/such/file.txt"))
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}
	if !strings.Contains(err.Error(), "/no/such/file.txt") {
		t.Errorf("error %q should contain the file path", err.Error())
	}
}

// TestReadFile_Truncation verifies that files larger than 64KB are
// truncated and the result ends with a [TRUNCATED] marker. This prevents
// a single large file from blowing up the LLM context window. The model
// sees [TRUNCATED] and can tell the user only a partial read was done.
func TestReadFile_Truncation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.txt")
	// Write 80KB of 'a' — well over the 64KB limit.
	_ = os.WriteFile(path, bytes.Repeat([]byte("a"), 80*1024), 0644)

	got, err := ReadFile.Run(makeReadFileArgs(path))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(got, "[TRUNCATED]") {
		t.Error("expected result to end with [TRUNCATED]")
	}
	// The content before [TRUNCATED] should be exactly 64KB of 'a'.
	content := strings.TrimSuffix(got, "\n[TRUNCATED]")
	if len(content) != maxReadBytes {
		t.Errorf("truncated content length: got %d, want %d", len(content), maxReadBytes)
	}
}

// TestReadFile_BinaryFile verifies that files containing null bytes are
// rejected with an error. Binary files (executables, images, etc.) would
// produce garbage text and waste context tokens, so we refuse them early.
func TestReadFile_BinaryFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "binary.bin")
	// Write some bytes with a null byte in the middle.
	_ = os.WriteFile(path, []byte("hello\x00world"), 0644)

	_, err := ReadFile.Run(makeReadFileArgs(path))
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
	_, err := ReadFile.Run("not json")
	if err == nil {
		t.Fatal("expected error for bad JSON, got nil")
	}
	if !strings.Contains(err.Error(), "bad args") {
		t.Errorf("error %q should contain 'bad args'", err.Error())
	}
}
