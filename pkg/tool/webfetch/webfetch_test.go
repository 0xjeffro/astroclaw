package webfetch

import (
	"context"
	"strings"
	"testing"
)

// Verify that extractText removes script, style, and noscript blocks, strips
// tags, and normalizes whitespace into clean readable text.
func TestExtractText(t *testing.T) {
	html := `<html>
<head><title>Test</title><style>body{color:red}</style></head>
<body>
<script>alert("xss")</script>
<h1>Hello World</h1>
<p>This is a <b>test</b> page.</p>
<noscript>Enable JavaScript</noscript>
</body></html>`

	got := extractText(html)

	if strings.Contains(got, "alert") {
		t.Error("script content should be removed")
	}
	if strings.Contains(got, "color:red") {
		t.Error("style content should be removed")
	}
	if strings.Contains(got, "Enable JavaScript") {
		t.Error("noscript content should be removed")
	}
	if !strings.Contains(got, "Hello World") {
		t.Error("heading text should be preserved")
	}
	if !strings.Contains(got, "test page") {
		t.Error("paragraph text should be preserved")
	}
}

// Verify that extractText handles empty input without panicking.
func TestExtractText_Empty(t *testing.T) {
	got := extractText("")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// Verify that formatJSON pretty-prints valid JSON.
func TestFormatJSON(t *testing.T) {
	input := []byte(`{"name":"test","value":42}`)
	got := formatJSON(input)

	if !strings.Contains(got, "  ") {
		t.Error("expected indented JSON output")
	}
	if !strings.Contains(got, `"name": "test"`) {
		t.Errorf("unexpected output: %s", got)
	}
}

// Verify that formatJSON returns the original string for invalid JSON.
func TestFormatJSON_Invalid(t *testing.T) {
	input := []byte(`not json at all`)
	got := formatJSON(input)
	if got != "not json at all" {
		t.Errorf("expected original string, got %q", got)
	}
}

// Verify that looksLikeHTML correctly detects HTML content.
func TestLooksLikeHTML(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"doctype", "<!DOCTYPE html><html>", true},
		{"html tag", "<html><body>hi</body></html>", true},
		{"plain text", "Hello world", false},
		{"json", `{"key": "value"}`, false},
		{"empty", "", false},
		{"case insensitive", "<!doctype html>", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := looksLikeHTML(tc.body); got != tc.want {
				t.Errorf("looksLikeHTML(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}

// Verify that Execute blocks private/local URLs (SSRF protection).
func TestExecute_SSRFBlocked(t *testing.T) {
	tool := New()

	blocked := []struct {
		name string
		url  string
	}{
		{"localhost", `{"url": "http://localhost/secret"}`},
		{"loopback IP", `{"url": "http://127.0.0.1/secret"}`},
		{"AWS metadata", `{"url": "http://169.254.169.254/latest/meta-data/"}`},
		{"private 10.x", `{"url": "http://10.0.0.1/internal"}`},
		{"private 192.168.x", `{"url": "http://192.168.1.1/admin"}`},
	}

	for _, tc := range blocked {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tool.Execute(context.Background(), tc.url)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.HasPrefix(result, "error") {
				t.Errorf("expected error for private URL, got %q", result)
			}
		})
	}
}

// Verify that Execute rejects non-http(s) schemes.
func TestExecute_BadScheme(t *testing.T) {
	tool := New()

	result, err := tool.Execute(context.Background(), `{"url": "ftp://example.com/file"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(result, "error") {
		t.Errorf("expected error for ftp scheme, got %q", result)
	}
}

// Verify that Execute rejects empty URL.
func TestExecute_EmptyURL(t *testing.T) {
	tool := New()

	result, err := tool.Execute(context.Background(), `{"url": ""}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(result, "error") {
		t.Errorf("expected error for empty URL, got %q", result)
	}
}

// Verify that Execute rejects invalid JSON args.
func TestExecute_InvalidJSON(t *testing.T) {
	tool := New()

	result, err := tool.Execute(context.Background(), `not json`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(result, "error") {
		t.Errorf("expected error for invalid JSON, got %q", result)
	}
}

// Verify that formatFetchResult includes all metadata fields.
func TestFormatFetchResult(t *testing.T) {
	result := formatFetchResult("https://example.com", 200, "html_to_text", true, "some content")

	for _, want := range []string{"https://example.com", "200", "html_to_text", "truncated", "some content"} {
		if !strings.Contains(result, want) {
			t.Errorf("result missing %q", want)
		}
	}
}

// Integration test: actually fetches a public URL. Run with:
//
//	go test ./pkg/tool/webfetch/ -run TestCanaryFetch -v
//
// Validates that the HTTP client, SSRF checks, and content extraction
// work against a real web server.
func TestCanaryFetch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tool := New()
	result, err := tool.Execute(context.Background(), `{"url": "https://example.com"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.HasPrefix(result, "error") {
		t.Fatalf("fetch failed: %s", result)
	}
	if !strings.Contains(result, "Example Domain") {
		t.Error("expected page to contain 'Example Domain'")
	}
	t.Logf("result length: %d chars", len(result))
	t.Logf("result:\n%s", result)
}
