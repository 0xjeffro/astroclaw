package tool

import "time"

var GetCurrentTime = Tool{
	Name:        "get_current_time",
	Description: "Returns the current date and time in ISO 8601 format.(RFC 3339 format, e.g. 2024-06-01T12:34:56Z)",
	Parameters: map[string]any{
		"type":       "object",
		"properties": map[string]any{},
		"required":   []string{},
	},
	Run: func(_ string) (string, error) {
		return time.Now().Format(time.RFC3339), nil
	},
}
