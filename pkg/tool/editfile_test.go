package tool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeEditFileArgs is a test helper that builds the JSON arguments for
// edit_file.
func makeEditFileArgs(path, oldText, newText string) string {
	b, _ := json.Marshal(map[string]any{
		"path":     path,
		"old_text": oldText,
		"new_text": newText,
	})
	return string(b)
}

// setupEditFile is a test helper that creates a temp file with the given
// content and returns its path. The file is automatically cleaned up
// when the test ends.
func setupEditFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.txt")
	_ = os.WriteFile(path, []byte(content), 0644)
	return path
}

// TestEditFile_SingleReplace verifies the happy path: old_text appears
// exactly once and is replaced with new_text. The rest of the file
// content must remain untouched.
func TestEditFile_SingleReplace(t *testing.T) {
	path := setupEditFile(t, "hello world")

	_, err := EditFile.Run(makeEditFileArgs(path, "world", "Go"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "hello Go" {
		t.Errorf("got %q, want %q", string(data), "hello Go")
	}
}

// TestEditFile_DeleteText verifies that passing an empty new_text
// effectively deletes old_text from the file. This is the intended
// way to remove code — the model explicitly passes "" as new_text.
func TestEditFile_DeleteText(t *testing.T) {
	path := setupEditFile(t, "aaa bbb ccc")

	_, err := EditFile.Run(makeEditFileArgs(path, " bbb", ""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "aaa ccc" {
		t.Errorf("got %q, want %q", string(data), "aaa ccc")
	}
}

// TestEditFile_NotFound verifies that old_text not present in the file
// returns an error. The file must remain unchanged.
func TestEditFile_NotFound(t *testing.T) {
	path := setupEditFile(t, "hello world")

	_, err := EditFile.Run(makeEditFileArgs(path, "xyz", "abc"))
	if err == nil {
		t.Fatal("expected error for old_text not found")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error %q should mention 'not found'", err.Error())
	}

	// File must be unchanged.
	data, _ := os.ReadFile(path)
	if string(data) != "hello world" {
		t.Errorf("file should be unchanged: got %q", string(data))
	}
}

// TestEditFile_AmbiguousMatch verifies that old_text appearing more than
// once is rejected. This forces the model to provide more surrounding
// context to make the match unique, preventing accidental edits to the
// wrong occurrence.
func TestEditFile_AmbiguousMatch(t *testing.T) {
	path := setupEditFile(t, "aaa bbb aaa")

	_, err := EditFile.Run(makeEditFileArgs(path, "aaa", "ccc"))
	if err == nil {
		t.Fatal("expected error for ambiguous match")
	}
	if !strings.Contains(err.Error(), "2 times") {
		t.Errorf("error %q should mention '2 times'", err.Error())
	}

	// File must be unchanged.
	data, _ := os.ReadFile(path)
	if string(data) != "aaa bbb aaa" {
		t.Errorf("file should be unchanged: got %q", string(data))
	}
}

// TestEditFile_MultilineReplace verifies that old_text and new_text can
// span multiple lines. This is the common case for real code edits —
// the model sends a few lines of context to uniquely match, then
// replaces them with modified lines.
func TestEditFile_MultilineReplace(t *testing.T) {
	original := "line1\nline2\nline3\nline4\n"
	path := setupEditFile(t, original)

	_, err := EditFile.Run(makeEditFileArgs(path, "line2\nline3", "lineA\nlineB\nlineC"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(path)
	want := "line1\nlineA\nlineB\nlineC\nline4\n"
	if string(data) != want {
		t.Errorf("got %q, want %q", string(data), want)
	}
}

// TestEditFile_FileNotExist verifies that editing a non-existent file
// returns an error with the file path.
func TestEditFile_FileNotExist(t *testing.T) {
	_, err := EditFile.Run(makeEditFileArgs("/no/such/file.txt", "a", "b"))
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
	if !strings.Contains(err.Error(), "read file") {
		t.Errorf("error %q should mention 'read file'", err.Error())
	}
}

// TestEditFile_BadJSON verifies that malformed JSON arguments return
// an error rather than panicking.
func TestEditFile_BadJSON(t *testing.T) {
	_, err := EditFile.Run("not json")
	if err == nil {
		t.Fatal("expected error for bad JSON, got nil")
	}
	if !strings.Contains(err.Error(), "bad args") {
		t.Errorf("error %q should contain 'bad args'", err.Error())
	}
}
