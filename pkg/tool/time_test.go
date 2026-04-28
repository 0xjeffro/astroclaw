package tool

import (
	"context"
	"testing"
	"time"
)

// TestGetCurrentTime_ReturnsValidRFC3339 verifies that the tool returns a
// string that can be parsed back as RFC 3339. We don't assert the exact
// time (that would make the test flaky), only that the format is correct.
// If this test fails, it means the Format call in time.go is using the
// wrong layout — which would cause the model to receive a time string it
// may not be able to interpret reliably.
func TestGetCurrentTime_ReturnsValidRFC3339(t *testing.T) {
	got, err := (&TimeTool{}).Execute(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := time.Parse(time.RFC3339, got); err != nil {
		t.Errorf("result %q is not valid RFC 3339: %v", got, err)
	}
}
