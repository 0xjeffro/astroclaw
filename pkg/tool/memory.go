package tool

import (
	"context"
	"encoding/json"
	"fmt"
)

// MemoryStore is the interface that MemorySaveTool needs to persist memories.
type MemoryStore interface {
	SaveMemory(ctx context.Context, agentID, content, sessionID string, messageIDs []string) error
}

// MemorySaveTool saves durable facts to persistent memory.
// The store, agentID, and sessionID are injected when the tool is created,
// typically per-session when building the Agent.
type MemorySaveTool struct {
	Store     MemoryStore
	AgentID   string
	SessionID string
}

func (t *MemorySaveTool) Name() string { return "memory_save" }
func (t *MemorySaveTool) Description() string {
	return "Save a durable fact to persistent memory. Memories are injected into future sessions, " +
		"so keep them compact and focused on facts that will still matter later. " +
		"Write as declarative facts, not instructions. " +
		"Example: 'User prefers concise responses' (good), 'Always respond concisely' (bad). " +
		"Save proactively when: user corrects you, shares a preference, or you discover something about the environment."
}
func (t *MemorySaveTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content": map[string]any{
				"type":        "string",
				"description": "The fact to remember. Write as a declarative statement.",
			},
		},
		"required": []string{"content"},
	}
}
func (t *MemorySaveTool) Execute(ctx context.Context, args string) *ToolResult {
	var p struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return &ToolResult{
			IsError: true,
			ForLLM:  fmt.Sprintf("bad args: %s", err),
			Err:     err,
		}
	}
	if p.Content == "" {
		return &ToolResult{
			IsError: true,
			ForLLM:  "content cannot be empty",
			Err:     fmt.Errorf("content cannot be empty"),
		}
	}
	if err := t.Store.SaveMemory(ctx, t.AgentID, p.Content, t.SessionID, nil); err != nil {
		return &ToolResult{
			IsError: true,
			ForLLM:  fmt.Sprintf("save memory: %s", err),
			Err:     err,
		}
	}
	return &ToolResult{
		ForLLM: "memory saved",
	}
}
func (t *MemorySaveTool) Approval() bool  { return false }
func (t *MemorySaveTool) Workspace() bool { return false }
