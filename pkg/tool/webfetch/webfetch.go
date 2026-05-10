package webfetch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	fetchTimeout = 60 * time.Second

	// Maximum response body size. Prevents memory issues from huge pages.
	// 10MB matches Picoclaw and Claude Code's limits.
	maxResponseBytes int64 = 10 * 1024 * 1024

	// Maximum characters returned to the agent after extraction.
	// Larger values waste context window budget.
	// TODO: current strategy is simple truncation. Future improvements:
	//   1. Summarize with a small/cheap model before returning
	//   2. Store full content in S3, inject only a summary into context
	//   3. Return a partial preview, let the agent read more on demand
	//      (similar to a filesystem where the agent can seek into the content)
	defaultMaxChars = 50000

	// User agents: first try looks like a normal browser,
	// retry on Cloudflare challenge uses an honest identifier.
	browserUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"
	honestUserAgent  = "astroclaw (AI assistant; +https://github.com/astroclaw/astroclaw)"
)

// Regexes for HTML text extraction.
var (
	reScript     = regexp.MustCompile(`(?is)<script[\s\S]*?</script>`)
	reStyle      = regexp.MustCompile(`(?is)<style[\s\S]*?</style>`)
	reNoscript   = regexp.MustCompile(`(?is)<noscript[\s\S]*?</noscript>`)
	reTags       = regexp.MustCompile(`<[^>]+>`)
	reWhitespace = regexp.MustCompile(`[^\S\n]+`)
	reBlankLines = regexp.MustCompile(`\n{3,}`)
)

// Tool implements the tool.Tool interface for fetching web page content.
type Tool struct {
	client *http.Client
}

func New() *Tool {
	return &Tool{
		client: newSafeHTTPClient(fetchTimeout),
	}
}

func (t *Tool) Name() string { return "web_fetch" }

func (t *Tool) Description() string {
	return "Fetch a web page and extract its text content. " +
		"Returns the page content as plain text. " +
		"Use this to read articles, documentation, or any web page the agent needs."
}

func (t *Tool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "URL to fetch (http or https)",
			},
		},
		"required": []string{"url"},
	}
}

func (t *Tool) Approval() bool  { return false }
func (t *Tool) Workspace() bool { return false }

func (t *Tool) Execute(ctx context.Context, args string) (string, error) {
	var parsed struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(args), &parsed); err != nil {
		return "error: invalid arguments", nil
	}

	rawURL := strings.TrimSpace(parsed.URL)
	if rawURL == "" {
		return "error: url is required", nil
	}

	// Validate URL scheme and structure.
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Sprintf("error: invalid URL: %v", err), nil
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return "error: only http and https URLs are allowed", nil
	}
	if parsedURL.Host == "" {
		return "error: missing host in URL", nil
	}

	// SSRF pre-flight: block obvious private hosts before making any request.
	if isObviousPrivateHost(parsedURL.Hostname()) {
		return "error: fetching private or local network hosts is not allowed", nil
	}

	maxChars := defaultMaxChars

	// First attempt with browser User-Agent.
	resp, body, err := t.doFetch(ctx, rawURL, browserUserAgent)
	if err != nil {
		return fmt.Sprintf("error: %v", err), nil
	}

	// Cloudflare bot detection: 403 + Cf-Mitigated header means the WAF
	// blocked us. Retry with an honest User-Agent that some operators whitelist.
	// Reference: picoclaw/pkg/tools/integration/web.go (lines 1720-1743)
	if resp.StatusCode == http.StatusForbidden && resp.Header.Get("Cf-Mitigated") == "challenge" {
		resp2, body2, err2 := t.doFetch(ctx, rawURL, honestUserAgent)
		if err2 == nil && resp2 != nil {
			resp = resp2
			body = body2
		}
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("error: HTTP %d from %s", resp.StatusCode, rawURL), nil
	}

	// Detect content type and process accordingly.
	text, extractor := t.processContent(resp, body)

	// Truncate if needed.
	truncated := false
	if len(text) > maxChars {
		text = text[:maxChars]
		truncated = true
	}

	return formatFetchResult(rawURL, resp.StatusCode, extractor, truncated, text), nil
}

// doFetch performs an HTTP GET with the given User-Agent and returns the
// response and body. The response body is limited to maxResponseBytes.
func (t *Tool) doFetch(ctx context.Context, rawURL, userAgent string) (*http.Response, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html, application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, nil, fmt.Errorf("read response: %w", err)
	}

	return resp, body, nil
}

// processContent detects the content type and converts the body to text.
// Returns the extracted text and the extractor name. The extractor is a
// string label that tells the agent how the content was processed:
//   - "json", response was JSON, pretty-printed with indentation
//   - "html_to_text", response was HTML, tags stripped and whitespace normalized
//   - "raw", response returned as-is
func (t *Tool) processContent(resp *http.Response, body []byte) (string, string) {
	contentType := resp.Header.Get("Content-Type")
	mediaType, _, _ := mime.ParseMediaType(contentType)

	switch {
	case mediaType == "application/json":
		return formatJSON(body), "json"

	case mediaType == "text/html" || looksLikeHTML(string(body)):
		return extractText(string(body)), "html_to_text"

	default:
		return string(body), "raw"
	}
}

// formatJSON pretty-prints JSON. If the input is not valid JSON, returns it as-is.
func formatJSON(body []byte) string {
	var data any
	if err := json.Unmarshal(body, &data); err != nil {
		return string(body)
	}
	formatted, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return string(body)
	}
	return string(formatted)
}

// extractText converts HTML to plain text by removing script/style/noscript
// blocks, stripping all tags, and normalizing whitespace.
// TODO: upgrade to proper HTML-to-markdown conversion for better readability.
func extractText(htmlContent string) string {
	// Remove content that should never appear in extracted text.
	result := reScript.ReplaceAllLiteralString(htmlContent, "")
	result = reStyle.ReplaceAllLiteralString(result, "")
	result = reNoscript.ReplaceAllLiteralString(result, "")

	// Strip remaining HTML tags.
	result = reTags.ReplaceAllLiteralString(result, "")

	// Normalize whitespace: collapse horizontal whitespace, limit blank lines.
	result = reWhitespace.ReplaceAllString(result, " ")
	result = reBlankLines.ReplaceAllString(result, "\n\n")

	// Clean up each line.
	lines := strings.Split(result, "\n")
	var clean []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			clean = append(clean, line)
		}
	}

	return strings.Join(clean, "\n")
}

// looksLikeHTML checks if the content starts with common HTML markers.
func looksLikeHTML(body string) bool {
	lower := strings.ToLower(strings.TrimSpace(body))
	return strings.HasPrefix(lower, "<!doctype") || strings.HasPrefix(lower, "<html")
}

// formatFetchResult builds the final text returned to the agent.
func formatFetchResult(url string, status int, extractor string, truncated bool, text string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("URL: %s\n", url))
	b.WriteString(fmt.Sprintf("Status: %d\n", status))
	b.WriteString(fmt.Sprintf("Extractor: %s\n", extractor))
	if truncated {
		b.WriteString("Note: content was truncated due to size limit\n")
	}
	b.WriteString(fmt.Sprintf("Content length: %d chars\n\n", len(text)))
	b.WriteString(text)
	return b.String()
}
