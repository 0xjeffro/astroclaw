package websearch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	// DuckDuckGo's HTML-only endpoint. This returns a lightweight HTML page
	// without JavaScript, making it easier to parse than the main site.
	ddgBaseURL = "https://html.duckduckgo.com/html/"

	// Chrome user agent. DuckDuckGo blocks requests with bot-like user agents.
	ddgUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"

	ddgTimeout = 10 * time.Second

	// Maximum response body size to prevent memory issues from unusually large pages.
	ddgMaxResponseBytes = 2 * 1024 * 1024 // 2MB
)

// Regex patterns for extracting results from DuckDuckGo's HTML response.
// These match the structure of html.duckduckgo.com, which uses CSS classes
// like "result__a" for links and "result__snippet" for descriptions.
// NOTE: DuckDuckGo can change their HTML structure at any time, which would
// break these patterns. There is no stability guarantee.
// TODO: consider fetching patterns from a remote JSON config so they can
// be updated without redeploying. Fall back to these defaults if fetch fails.
var (
	reDDGLink = regexp.MustCompile(
		`<a[^>]*class="[^"]*result__a[^"]*"[^>]*href="([^"]+)"[^>]*>([\s\S]*?)</a>`,
	)
	reDDGSnippet = regexp.MustCompile(
		`<a[^>]*class="[^"]*result__snippet[^"]*"[^>]*>([\s\S]*?)</a>`,
	)
	reHTMLTags = regexp.MustCompile(`<[^>]+>`)
)

// DuckDuckGoProvider searches via DuckDuckGo's HTML endpoint.
// No API key required. Free but subject to rate limiting and bot detection.
type DuckDuckGoProvider struct {
	client *http.Client
}

func NewDuckDuckGoProvider() *DuckDuckGoProvider {
	return &DuckDuckGoProvider{
		client: &http.Client{
			Timeout: ddgTimeout,
		},
	}
}

func (p *DuckDuckGoProvider) Name() string { return "duckduckgo" }

func (p *DuckDuckGoProvider) Search(ctx context.Context, query string, count int, timeRange string) ([]SearchResult, error) {
	searchURL := ddgBaseURL + "?q=" + url.QueryEscape(query)
	if df := ddgTimeRange(timeRange); df != "" {
		searchURL += "&df=" + url.QueryEscape(df)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", ddgUserAgent)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// DuckDuckGo returns 200 even for errors/captchas, so we can't rely
	// on status codes alone. We check the parsed results instead.
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, ddgMaxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	return parseDDGResults(string(body), count), nil
}

// parseDDGResults extracts search results from DuckDuckGo's HTML response.
// Link and snippet matches are aligned by position (the Nth link corresponds
// to the Nth snippet). This is fragile but matches DuckDuckGo's current HTML
// structure where links and snippets appear in the same order.
func parseDDGResults(html string, count int) []SearchResult {
	linkMatches := reDDGLink.FindAllStringSubmatch(html, -1)
	snippetMatches := reDDGSnippet.FindAllStringSubmatch(html, -1)

	if len(linkMatches) == 0 {
		return nil
	}

	var results []SearchResult
	for i, match := range linkMatches {
		if i >= count {
			break
		}

		rawURL := match[1]
		title := stripHTML(match[2])

		// DuckDuckGo wraps external URLs in a redirect:
		// //duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com&rut=...
		// Extract the actual URL from the uddg parameter.
		actualURL := extractDDGURL(rawURL)

		result := SearchResult{
			Title: title,
			URL:   actualURL,
		}

		// Snippets align with links by position.
		if i < len(snippetMatches) {
			snippet := stripHTML(snippetMatches[i][1])
			result.Snippet = snippet
		}

		if result.Title != "" && result.URL != "" {
			results = append(results, result)
		}
	}

	return results
}

// extractDDGURL extracts the actual URL from DuckDuckGo's redirect wrapper.
// DuckDuckGo wraps URLs like: //duckduckgo.com/l/?uddg=<encoded_url>&rut=...
// If the URL doesn't contain "uddg=", it's returned as-is.
//
// We use url.Parse + Query().Get() instead of manual string splitting because
// the original URL inside uddg may itself contain & characters (e.g.,
// https://example.com/search?q=go&page=2). Manual splitting after full
// url.QueryUnescape would incorrectly truncate these parameters.
// url.Parse understands that uddg's value is percent-encoded as a single
// query parameter, so it decodes only the value boundary correctly.
func extractDDGURL(rawURL string) string {
	if !strings.Contains(rawURL, "uddg=") {
		return rawURL
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	uddg := parsed.Query().Get("uddg")
	if uddg != "" {
		return uddg
	}

	return rawURL
}

// ddgTimeRange maps our normalized time range codes to DuckDuckGo's df parameter.
// DuckDuckGo uses "t" for year instead of "y".
func ddgTimeRange(code string) string {
	switch code {
	case "d":
		return "d"
	case "w":
		return "w"
	case "m":
		return "m"
	case "y":
		return "t" // DuckDuckGo uses "t" for past year
	default:
		return ""
	}
}

// stripHTML removes all HTML tags and normalizes whitespace.
func stripHTML(s string) string {
	s = reHTMLTags.ReplaceAllString(s, "")
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(s)
}
