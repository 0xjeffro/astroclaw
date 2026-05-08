package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// SearchProvider is the interface that all search backends implement.
// Each provider handles its own HTTP requests, authentication, and
// response parsing.
type SearchProvider interface {
	// Search executes a web search and returns formatted results.
	// Count is the max number of results to return (1-10).
	// timeRange is an optional filter: "d" (day), "w" (week), "m" (month), "y" (year), or "" (none).
	Search(ctx context.Context, query string, count int, timeRange string) ([]SearchResult, error)

	// Name returns the provider name for logging and diagnostics.
	Name() string
}

type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"`
}

// Tool implements the tool.Tool interface. The agent sees a single
// "web_search" tool. Which backend is used depends on how the tool is configured.
type Tool struct {
	Provider   SearchProvider
	MaxResults int // default max results if the model doesn't specify count
}

func (t *Tool) Name() string { return "web_search" }

func (t *Tool) Description() string {
	return "Search the web for current information. Returns titles, URLs, and snippets. " +
		"Use this for facts, news, documentation, or anything that might be outdated in your training data."
}

func (t *Tool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query",
			},
			"count": map[string]any{
				"type":        "integer",
				"description": "Number of results to return (default 5, max 100)",
				"minimum":     1.0,
				"maximum":     100.0,
			},
			"time_range": map[string]any{
				"type":        "string",
				"description": "Optional time filter: d (past day), w (past week), m (past month), y (past year)",
				"enum":        []string{"d", "w", "m", "y"},
			},
		},
		"required": []string{"query"},
	}
}

func (t *Tool) Approval() bool  { return false }
func (t *Tool) Workspace() bool { return false }

func (t *Tool) Execute(ctx context.Context, args string) (string, error) {
	var parsed struct {
		Query     string `json:"query"`
		Count     int    `json:"count"`
		TimeRange string `json:"time_range"`
	}
	if err := json.Unmarshal([]byte(args), &parsed); err != nil {
		return "error: invalid arguments", nil
	}

	query := strings.TrimSpace(parsed.Query)
	if query == "" {
		return "error: query is required", nil
	}

	if t.Provider == nil {
		return "error: web search is not configured. No search provider available.", nil
	}

	// Determine result count.
	maxResults := t.MaxResults
	if maxResults <= 0 {
		maxResults = 5
	}
	count := maxResults
	if parsed.Count > 0 && parsed.Count <= 100 {
		count = parsed.Count
	}

	// Normalize time range. Invalid values default to no filter rather than
	// returning an error, so the search still returns useful results.
	timeRange := strings.ToLower(strings.TrimSpace(parsed.TimeRange))
	switch timeRange {
	case "d", "w", "m", "y":
		// valid
	default:
		timeRange = ""
	}

	results, err := t.Provider.Search(ctx, query, count, timeRange)
	if err != nil {
		return fmt.Sprintf("error: search failed: %v", err), nil
	}

	if len(results) == 0 {
		return fmt.Sprintf("No results found for: %s", query), nil
	}

	return formatResults(query, results), nil
}

func formatResults(query string, results []SearchResult) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Search results for: %s\n\n", query))
	for i, r := range results {
		b.WriteString(fmt.Sprintf("%d. %s\n   %s\n", i+1, r.Title, r.URL))
		if r.Snippet != "" {
			b.WriteString(fmt.Sprintf("   %s\n", r.Snippet))
		}
		if i < len(results)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
