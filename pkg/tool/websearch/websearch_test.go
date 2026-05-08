package websearch

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// fakeProvider returns canned results for testing the Tool logic
// without hitting any real search engine.
type fakeProvider struct {
	results []SearchResult
	err     error

	// captured args from the last call
	lastQuery     string
	lastCount     int
	lastTimeRange string
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) Search(_ context.Context, query string, count int, timeRange string) ([]SearchResult, error) {
	f.lastQuery = query
	f.lastCount = count
	f.lastTimeRange = timeRange
	return f.results, f.err
}

// Verify that a basic search passes query, default count, and empty time range to the provider.
func TestExecute_BasicSearch(t *testing.T) {
	fp := &fakeProvider{
		results: []SearchResult{
			{Title: "Result 1", URL: "https://example.com/1", Snippet: "First result"},
			{Title: "Result 2", URL: "https://example.com/2", Snippet: "Second result"},
		},
	}
	tool := &Tool{Provider: fp, MaxResults: 5}

	result, err := tool.Execute(context.Background(), `{"query": "test"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fp.lastQuery != "test" {
		t.Errorf("query = %q, want %q", fp.lastQuery, "test")
	}
	if fp.lastCount != 5 {
		t.Errorf("count = %d, want 5 (default)", fp.lastCount)
	}
	if fp.lastTimeRange != "" {
		t.Errorf("timeRange = %q, want empty", fp.lastTimeRange)
	}
	if result == "" {
		t.Error("result is empty")
	}
}

// Verify that the model can override the default result count.
func TestExecute_CountOverride(t *testing.T) {
	fp := &fakeProvider{}
	tool := &Tool{Provider: fp, MaxResults: 5}

	if _, err := tool.Execute(context.Background(), `{"query": "test", "count": 3}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fp.lastCount != 3 {
		t.Errorf("count = %d, want 3", fp.lastCount)
	}
}

// Verify that count values over 100 are rejected and the default is used instead.
func TestExecute_CountMax(t *testing.T) {
	fp := &fakeProvider{}
	tool := &Tool{Provider: fp, MaxResults: 5}

	if _, err := tool.Execute(context.Background(), `{"query": "test", "count": 200}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fp.lastCount != 5 {
		t.Errorf("count = %d, want 5 (default, because 200 > 100)", fp.lastCount)
	}
}

// Verify that count=0 falls back to the default (0 is not a valid count).
func TestExecute_CountZero(t *testing.T) {
	fp := &fakeProvider{}
	tool := &Tool{Provider: fp, MaxResults: 5}

	if _, err := tool.Execute(context.Background(), `{"query": "test", "count": 0}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fp.lastCount != 5 {
		t.Errorf("count = %d, want 5 (default, because 0 is invalid)", fp.lastCount)
	}
}

// Verify that a valid time_range is passed through to the provider.
func TestExecute_TimeRange(t *testing.T) {
	fp := &fakeProvider{}
	tool := &Tool{Provider: fp, MaxResults: 5}

	if _, err := tool.Execute(context.Background(), `{"query": "test", "time_range": "w"}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fp.lastTimeRange != "w" {
		t.Errorf("timeRange = %q, want %q", fp.lastTimeRange, "w")
	}
}

// Verify that an invalid time_range defaults to no filter instead of returning an error.
func TestExecute_InvalidTimeRange(t *testing.T) {
	fp := &fakeProvider{}
	tool := &Tool{Provider: fp, MaxResults: 5}

	if _, err := tool.Execute(context.Background(), `{"query": "test", "time_range": "x"}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fp.lastQuery != "test" {
		t.Error("provider should have been called even with invalid time_range")
	}
	if fp.lastTimeRange != "" {
		t.Errorf("timeRange = %q, want empty (invalid value should default to no filter)", fp.lastTimeRange)
	}
}

// Verify that a whitespace-only query returns an error without calling the provider.
func TestExecute_EmptyQuery(t *testing.T) {
	fp := &fakeProvider{}
	tool := &Tool{Provider: fp, MaxResults: 5}

	result, err := tool.Execute(context.Background(), `{"query": "  "}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fp.lastQuery != "" {
		t.Error("provider should not have been called with empty query")
	}
	if !strings.HasPrefix(result, "error") {
		t.Errorf("expected error message, got %q", result)
	}
}

// Verify that malformed JSON args return an error.
func TestExecute_InvalidJSON(t *testing.T) {
	tool := &Tool{Provider: &fakeProvider{}, MaxResults: 5}

	result, err := tool.Execute(context.Background(), `not json`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(result, "error") {
		t.Errorf("expected error message, got %q", result)
	}
}

// Verify that calling Execute with no provider configured returns a helpful error.
func TestExecute_NoProvider(t *testing.T) {
	tool := &Tool{Provider: nil, MaxResults: 5}

	result, err := tool.Execute(context.Background(), `{"query": "test"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(result, "error") {
		t.Errorf("expected error message, got %q", result)
	}
}

// Verify that provider errors are surfaced in the tool result.
func TestExecute_ProviderError(t *testing.T) {
	fp := &fakeProvider{err: fmt.Errorf("network timeout")}
	tool := &Tool{Provider: fp, MaxResults: 5}

	result, err := tool.Execute(context.Background(), `{"query": "test"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(result, "error") {
		t.Errorf("expected error message, got %q", result)
	}
	if !strings.Contains(result, "network timeout") {
		t.Errorf("error message should contain cause, got %q", result)
	}
}

// Verify that an empty result set returns a human-readable "no results" message.
func TestExecute_NoResults(t *testing.T) {
	fp := &fakeProvider{results: nil}
	tool := &Tool{Provider: fp, MaxResults: 5}

	result, err := tool.Execute(context.Background(), `{"query": "obscure query"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "No results found for: obscure query" {
		t.Errorf("unexpected result: %q", result)
	}
}

// Verify that when MaxResults is not set (zero value), the default of 5 is used.
func TestExecute_DefaultMaxResults(t *testing.T) {
	fp := &fakeProvider{}
	tool := &Tool{Provider: fp} // MaxResults not set (zero value)

	if _, err := tool.Execute(context.Background(), `{"query": "test"}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fp.lastCount != 5 {
		t.Errorf("count = %d, want 5 (default when MaxResults is 0)", fp.lastCount)
	}
}

// Verify that formatResults includes query, titles, URLs, and snippets in the output.
func TestFormatResults(t *testing.T) {
	results := []SearchResult{
		{Title: "Go Blog", URL: "https://go.dev/blog", Snippet: "The Go blog"},
		{Title: "Go Docs", URL: "https://go.dev/doc"},
	}

	output := formatResults("golang", results)

	if output == "" {
		t.Fatal("output is empty")
	}
	for _, want := range []string{"golang", "Go Blog", "https://go.dev/blog", "The Go blog", "Go Docs", "https://go.dev/doc"} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q", want)
		}
	}
}
