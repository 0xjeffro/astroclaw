package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeWriteFileArgs is a test helper that builds the JSON arguments for
// write_file. overwrite is optional — pass true to set it.
func makeWriteFileArgs(path, content string, overwrite bool) string {
	b, _ := json.Marshal(map[string]any{
		"path":      path,
		"content":   content,
		"overwrite": overwrite,
	})
	return string(b)
}

// TestWriteFile_NewFile verifies the happy path: writing to a file that
// does not yet exist. The file should be created with the exact content.
func TestWriteFile_NewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.txt")

	got, err := (&WriteFileTool{}).Execute(context.Background(), makeWriteFileArgs(path, "hello world", false))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "11 bytes") {
		t.Errorf("result %q should mention byte count", got)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "hello world" {
		t.Errorf("file content: got %q, want %q", string(data), "hello world")
	}
}

// TestWriteFile_RefuseOverwrite verifies that writing to an existing file
// WITHOUT overwrite=true returns an error. This is the core safety
// mechanism — the model must explicitly opt in to overwriting.
func TestWriteFile_RefuseOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.txt")
	_ = os.WriteFile(path, []byte("original"), 0644)

	_, err := (&WriteFileTool{}).Execute(context.Background(), makeWriteFileArgs(path, "replaced", false))
	if err == nil {
		t.Fatal("expected error when overwriting without overwrite=true")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error %q should mention 'already exists'", err.Error())
	}

	// Verify the original content was NOT changed.
	data, _ := os.ReadFile(path)
	if string(data) != "original" {
		t.Errorf("file should be unchanged: got %q, want %q", string(data), "original")
	}
}

// TestWriteFile_OverwriteExplicit verifies that overwrite=true allows
// replacing an existing file's content.
func TestWriteFile_OverwriteExplicit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.txt")
	_ = os.WriteFile(path, []byte("original"), 0644)

	_, err := (&WriteFileTool{}).Execute(context.Background(), makeWriteFileArgs(path, "replaced", true))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "replaced" {
		t.Errorf("file content: got %q, want %q", string(data), "replaced")
	}
}

// TestWriteFile_CreatesParentDirs verifies that parent directories are
// created automatically when they don't exist. The model should be able
// to write to "newpkg/subdir/foo.go" without having to mkdir first.
func TestWriteFile_CreatesParentDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "c", "deep.txt")

	_, err := (&WriteFileTool{}).Execute(context.Background(), makeWriteFileArgs(path, "deep content", false))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "deep content" {
		t.Errorf("file content: got %q, want %q", string(data), "deep content")
	}
}

// TestWriteFile_BadJSON verifies that malformed JSON arguments return
// an error rather than panicking.
func TestWriteFile_BadJSON(t *testing.T) {
	_, err := (&WriteFileTool{}).Execute(context.Background(), "not json")
	if err == nil {
		t.Fatal("expected error for bad JSON, got nil")
	}
	if !strings.Contains(err.Error(), "bad args") {
		t.Errorf("error %q should contain 'bad args'", err.Error())
	}
}
