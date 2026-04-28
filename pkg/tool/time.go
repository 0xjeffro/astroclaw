package tool

import "time"

type TimeTool struct{}

func (t *TimeTool) Name() string { return "get_current_time" }
func (t *TimeTool) Description() string {
	return "Returns the current date and time in ISO 8601 format (RFC 3339 format, e.g. 2024-06-01T12:34:56Z)"
}
func (t *TimeTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
		"required":   []string{},
	}
}
func (t *TimeTool) Execute(_ string) (string, error) {
	return time.Now().Format(time.RFC3339), nil
}
func (t *TimeTool) Approval() bool  { return false }
func (t *TimeTool) Workspace() bool { return false }
