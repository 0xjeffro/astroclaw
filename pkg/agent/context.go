package agent

import (
	"encoding/json"
	"iclaw/pkg/provider"
)

const maxResponseTokens = 4096 // reserved for model's response

// isOverBudget checks whether the current history + system + summary + tools
// exceed the model's context window. Returns false if contextWindow is 0
// (no limit configured).
func (a *Agent) isOverBudget() bool {
	if a.contextWindow <= 0 {
		return false
	}
	systemTokens := len(a.system) * 2 / 5
	summaryTokens := len(a.summary) * 2 / 5
	historyTokens := estimateTokens(a.history)
	toolTokens := estimateToolTokens(toProviderTools(a.tools))
	total := systemTokens + summaryTokens + historyTokens + toolTokens + maxResponseTokens
	return total > a.contextWindow
}

// estimateMessageTokens estimates the token count of a single message
// using a character-based heuristic: ~2.5 characters per token, no external tokenizer needed.
// Counts: (Role + Content) ToolCall metadata (ID, name, arguments) + ToolCallID
// + 12 bytes fixed overhead per message (role tag, JSON wrapping).
func estimateMessageTokens(msg provider.Message) int {
	chars := len(msg.Role) + len(msg.Content)
	for _, tc := range msg.ToolCalls {
		chars += len(tc.ID)
		chars += len(tc.Function.Name)
		chars += len(tc.Function.Arguments)
	}
	chars += len(msg.ToolCallID)
	return chars*2/5 + 12
}

// estimateTokens estimates the total token count of a message slice.
func estimateTokens(msgs []provider.Message) int {
	total := 0
	for _, m := range msgs {
		total += estimateMessageTokens(m)
	}
	return total
}

// estimateToolTokens estimates how many tokens the tool definitions
// occupy in the request. Each tool's name, description, and JSON-serialized
// parameters are counted, plus 20 bytes overhead per tool for JSON structure.
func estimateToolTokens(tools []provider.Tool) int {
	total := 0
	for _, t := range tools {
		chars := len(t.Function.Name) + len(t.Function.Description)
		if params, err := json.Marshal(t.Function.Parameters); err == nil {
			chars += len(params)
		}
		total += chars*2/5 + 20
	}
	return total
}

// parseTurnBoundaries returns the indices of all user messages in history.
// Each user message marks the start of a new Turn — a complete cycle of:
// user → assistant (possibly with tool_calls → tool_results) → final response.
// Truncation must happen AT these boundaries to avoid splitting tool_call/tool_result pairs.
func parseTurnBoundaries(history []provider.Message) []int {
	var boundaries []int
	for i, m := range history {
		if m.Role == "user" {
			boundaries = append(boundaries, i)
		}
	}
	return boundaries
}

// findSafeBoundary finds the nearest Turn boundary at or before targetIndex.
//
// Normal case: returns the last boundary <= targetIndex. For example, if
// boundaries are [0, 4, 8] and targetIndex is 5, it returns 4.
//
// Edge case: if no boundary exists before targetIndex (e.g. history starts
// with non-user messages due to corruption or unusual state), it falls back
// to the first boundary AFTER targetIndex. This shouldn't happen in normal
// conversations (history always starts with a user message), but we handle
// it defensively rather than panicking.
//
// If history has no Turn boundaries at all (no user messages), returns 0.
//
// This ensures truncation always happens at a user message, keeping all
// tool_call/tool_result pairs intact within their Turn.
func findSafeBoundary(history []provider.Message, targetIndex int) int {
	boundaries := parseTurnBoundaries(history)
	if len(boundaries) == 0 {
		return 0
	}

	// Find the last boundary that is <= targetIndex.
	best := -1
	for _, b := range boundaries {
		if b <= targetIndex {
			best = b
		}
	}
	if best >= 0 {
		return best
	}

	// No boundary before targetIndex, return the first one after.
	return boundaries[0]
}
