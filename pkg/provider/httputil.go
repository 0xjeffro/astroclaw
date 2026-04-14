package provider

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	maxRetries   = 3
	maxRetryWait = 60 * time.Second
)

// doWithRetry sends an HTTP request and retries on transient failures
// (429 rate limit and 5xx server errors). On 429, it reads the Retry-After
// header to determine how long to wait; if absent, it falls back to
// linear backoff (1s, 2s, 3s). Retries are capped at 3 attempts.
//
// The body parameter is needed because http.Request.Body is consumed after
// each Do() call — we reset it from the original bytes on each retry.
//
// Sleeps are interruptible via the request's context, so Ctrl+C in the
// REPL won't hang during a retry wait.
func doWithRetry(client *http.Client, req *http.Request, body []byte) (*http.Response, error) {
	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Reset body for this attempt (consumed by previous Do call).
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))

		resp, err := client.Do(req)
		if err != nil {
			// Network errors (DNS, connection refused, etc.) are not retried —
			// they are unlikely to resolve in seconds.
			return nil, err
		}

		if !isRetryableStatus(resp.StatusCode) {
			return resp, nil
		}

		// Last attempt — return the error response as-is, let the caller
		// handle it (they'll read the body and wrap it in an error).
		if attempt == maxRetries {
			return resp, nil
		}

		// Parse retry delay, close the body (we won't use it), and wait.
		delay := retryDelay(resp, attempt)
		resp.Body.Close()

		// Sleep but respect context cancellation (e.g. Ctrl+C).
		select {
		case <-time.After(delay):
			// continue to next attempt
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}

	// Unreachable, but the compiler needs it.
	return nil, nil
}

// isRetryableStatus returns true for HTTP status codes that are worth
// retrying: 429 (rate limit) and 5xx (server errors).
func isRetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

// retryDelay determines how long to wait before the next retry attempt.
// Priority:
//  1. Retry-After header as numeric seconds (e.g. "30")
//  2. Retry-After header as HTTP date (e.g. "Sun, 13 Apr 2026 22:10:30 GMT")
//  3. Fallback: linear backoff = (attempt + 1) seconds
//
// The result is clamped to [0, 60s] to prevent absurdly long waits from
// a misbehaving API.
func retryDelay(resp *http.Response, attempt int) time.Duration {
	if after := resp.Header.Get("Retry-After"); after != "" {
		// Try numeric seconds first (most common).
		if secs, err := strconv.ParseInt(after, 10, 64); err == nil {
			return clampDelay(time.Duration(secs) * time.Second)
		}
		// Try HTTP date format (e.g. from some CDNs / proxies).
		if t, err := http.ParseTime(after); err == nil {
			return clampDelay(time.Until(t))
		}
	}

	// Fallback: linear backoff (1s, 2s, 3s).
	return clampDelay(time.Duration(attempt+1) * time.Second)
}

// clampDelay constrains a duration to [0, maxRetryWait].
func clampDelay(d time.Duration) time.Duration {
	if d < 0 {
		return 0
	}
	if d > maxRetryWait {
		return maxRetryWait
	}
	return d
}
