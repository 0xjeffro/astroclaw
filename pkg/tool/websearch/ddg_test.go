package websearch

import (
	"context"
	"testing"
)

// Test HTML parsing with a snapshot of DuckDuckGo's HTML structure.
// If DuckDuckGo changes their HTML, update this snapshot and the regexes.
func TestParseDDGResults(t *testing.T) {
	html := `
<div class="result results_links results_links_deep web-result">
  <div class="links_main links_deep result__body">
    <h2 class="result__title">
      <a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fblog%2Ferror-handling&amp;rut=abc123">Error handling in <b>Go</b></a>
    </h2>
    <a class="result__snippet" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fblog%2Ferror-handling&amp;rut=abc123">This article explains how <b>Go</b> handles errors using multiple return values.</a>
  </div>
</div>
<div class="result results_links results_links_deep web-result">
  <div class="links_main links_deep result__body">
    <h2 class="result__title">
      <a rel="nofollow" class="result__a" href="https://example.com/direct-link">Direct link example</a>
    </h2>
    <a class="result__snippet" href="https://example.com/direct-link">A result with a direct URL, no uddg wrapper.</a>
  </div>
</div>
<div class="result results_links results_links_deep web-result">
  <div class="links_main links_deep result__body">
    <h2 class="result__title">
      <a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fno-snippet&amp;rut=xyz">No snippet result</a>
    </h2>
  </div>
</div>
`

	results := parseDDGResults(html, 10)

	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}

	// First result: uddg-wrapped URL with HTML in title.
	r := results[0]
	if r.URL != "https://go.dev/blog/error-handling" {
		t.Errorf("result[0].URL = %q, want https://go.dev/blog/error-handling", r.URL)
	}
	if r.Title != "Error handling in Go" {
		t.Errorf("result[0].Title = %q, want %q", r.Title, "Error handling in Go")
	}
	if r.Snippet == "" {
		t.Error("result[0].Snippet is empty, expected a snippet")
	}

	// Second result: direct URL without uddg wrapper.
	r = results[1]
	if r.URL != "https://example.com/direct-link" {
		t.Errorf("result[1].URL = %q, want https://example.com/direct-link", r.URL)
	}

	// Third result: no snippet (only 2 snippets in HTML, 3 links).
	if len(results) >= 3 {
		r = results[2]
		if r.URL != "https://example.com/no-snippet" {
			t.Errorf("result[2].URL = %q, want https://example.com/no-snippet", r.URL)
		}
	}
}

// Verify that HTML with no matching result elements returns an empty slice.
func TestParseDDGResults_Empty(t *testing.T) {
	results := parseDDGResults("<html><body>no results</body></html>", 10)
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

// Verify that the count parameter limits how many results are returned.
func TestParseDDGResults_Count(t *testing.T) {
	html := `
<a class="result__a" href="https://example.com/1">Result 1</a>
<a class="result__snippet">Snippet 1</a>
<a class="result__a" href="https://example.com/2">Result 2</a>
<a class="result__snippet">Snippet 2</a>
<a class="result__a" href="https://example.com/3">Result 3</a>
<a class="result__snippet">Snippet 3</a>
`
	results := parseDDGResults(html, 2)
	if len(results) != 2 {
		t.Errorf("expected 2 results (count limit), got %d", len(results))
	}
}

// Verify that extractDDGURL correctly unwraps DuckDuckGo's redirect URLs
// and handles direct URLs, missing trailing params, and original URLs with query params.
func TestExtractDDGURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "uddg wrapped URL",
			input: "//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fdoc&rut=abc",
			want:  "https://go.dev/doc",
		},
		{
			name:  "direct URL",
			input: "https://example.com/page",
			want:  "https://example.com/page",
		},
		{
			name:  "uddg without trailing params",
			input: "//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com",
			want:  "https://example.com",
		},
		{
			name:  "uddg with query params in original URL",
			input: "//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fsearch%3Fq%3Dgo%26page%3D2&rut=abc123",
			want:  "https://example.com/search?q=go&page=2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDDGURL(tt.input)
			if got != tt.want {
				t.Errorf("extractDDGURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// Verify that time range codes map correctly, especially "y" -> "t" (DuckDuckGo's quirk).
func TestDDGTimeRange(t *testing.T) {
	if ddgTimeRange("y") != "t" {
		t.Error("year should map to 't' for DuckDuckGo")
	}
	if ddgTimeRange("d") != "d" {
		t.Error("day should map to 'd'")
	}
	if ddgTimeRange("") != "" {
		t.Error("empty should map to empty")
	}
}

// Verify that stripHTML removes tags and normalizes whitespace.
func TestStripHTML(t *testing.T) {
	got := stripHTML("Hello <b>world</b> and <i>more</i>")
	if got != "Hello world and more" {
		t.Errorf("stripHTML = %q, want %q", got, "Hello world and more")
	}
}

// Integration test: actually hits DuckDuckGo. Run with:
//
//	go test ./pkg/tool/websearch/ -run TestCanaryDDG -v
//
// This test validates that the regexes still work against DuckDuckGo's
// live HTML. If it fails, DuckDuckGo has likely changed their HTML
// structure and the regexes need to be updated.
func TestCanaryDDG(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	provider := NewDuckDuckGoProvider()
	results, err := provider.Search(context.Background(), "Go programming language", 3, "")
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("no results returned. DuckDuckGo HTML structure may have changed, check regexes")
	}

	for i, r := range results {
		if r.Title == "" {
			t.Errorf("result[%d].Title is empty", i)
		}
		if r.URL == "" {
			t.Errorf("result[%d].URL is empty", i)
		}
		t.Logf("result[%d]: %s - %s", i, r.Title, r.URL)
	}
}
