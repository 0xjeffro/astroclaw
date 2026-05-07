package agent

import (
	"astroclaw/pkg/provider"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const maxResponseTokens = 4096 // reserved for model's response

const summarizePrompt = `CRITICAL: Respond with TEXT ONLY. Do NOT call any tools.

  Summarize the conversation below. Structure your summary with these sections (skip any that are empty):

  1. Primary Request: what the user originally wanted to accomplish
  2. Key Technical Concepts: important technologies, frameworks, or patterns discussed
  3. Files and Code: which files were read, modified, or created, and what changed
  4. Errors and Fixes: any errors encountered and how they were resolved
  5. Progress: what was accomplished so far
  6. All User Preferences: any user preferences, corrections, or style guidance expressed
  7. Pending Tasks: anything explicitly asked for but not yet completed
  8. Current Work: what was being worked on most recently
  9. Next Steps: the likely next action based on the conversation

  Be concise but preserve all key facts: names, numbers, file paths, and decisions.
  Do NOT call any tools. Respond with only the summary.`

const mergePrompt = "Merge these two conversation summaries into one cohesive summary. Keep the same section structure. Respond with only the merged summary."

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

// forceCompress drops the oldest ~50% of Turns from history. This is the
// emergency strategy when summarization fails or history is still over
// budget after summarization. Always cuts at Turn boundaries to keep
// tool_call/tool_result pairs intact.
func (a *Agent) forceCompress() {
	// Too few messages to compress, not worth cutting.
	if len(a.history) <= 2 {
		return
	}

	boundaries := parseTurnBoundaries(a.history)

	var cutIndex int
	if len(boundaries) <= 1 {
		// Only 0 or 1 Turn, can't split by Turn. Fall back to keeping
		// only the last user message.
		for i := len(a.history) - 1; i >= 0; i-- {
			if a.history[i].Role == "user" {
				cutIndex = i
				break
			}
		}
	} else {
		// Cut at the midpoint Turn boundary, drops roughly the oldest 50%.
		cutIndex = boundaries[len(boundaries)/2]
	}

	if cutIndex <= 0 {
		return
	}

	dropped := cutIndex
	a.history = a.history[cutIndex:]

	// Record how many messages were dropped, so the model (and the summary)
	// know that earlier context was lost.
	note := fmt.Sprintf("[Emergency compression dropped %d oldest messages due to context limit]", dropped)
	if a.summary != "" {
		a.summary += "\n" + note
	} else {
		a.summary = note
	}
}

// summarizeOldTurns summarizes the oldest Turns in history using the LLM,
// keeping the most recent Turns as-is. The summary replaces the original
// messages, reducing token count while preserving key context.
func (a *Agent) summarizeOldTurns(ctx context.Context) error {
	// Not enough to summarize
	if len(a.history) <= 4 {
		return nil
	}

	// Find a safe cut point that keeps the last 2 Turns intact.
	boundaries := parseTurnBoundaries(a.history)
	if len(boundaries) < 2 {
		return nil
	}

	// Keep from the second-to-last Turn onward.
	keepFrom := boundaries[len(boundaries)-2]
	if keepFrom <= 0 {
		return nil
	}

	// Extract messages to summarize (everything before keepFrom).
	toSummarize := a.history[:keepFrom]

	// Filter: skip system messages, empty content, and oversized messages.
	// Tool results ARE included (small ones like timestamps are useful
	// context). Only truly huge messages get skipped via the size check.
	//
	// Why skip oversized messages entirely instead of truncating them:
	// Truncating a large tool result (e.g. 64KB of source code) would
	// keep the file header (imports, first few functions) which is
	// unlikely to be the relevant part. The assistant's reply that
	// follows the tool result already contains the useful conclusions
	// (e.g. "line 85 has a nil pointer bug"), and that reply is kept
	// in full. So skipping the raw tool output loses little, while
	// including it would risk blowing up the summarization request.
	maxMsgTokens := a.contextWindow / 2
	var filtered []provider.Message
	for _, m := range toSummarize {
		if m.Role == "system" {
			continue
		}
		if m.Content == "" {
			continue
		}
		if maxMsgTokens > 0 && estimateMessageTokens(m) > maxMsgTokens {
			continue
		}
		filtered = append(filtered, m)
	}
	if len(filtered) == 0 {
		return nil
	}

	// Build the conversation text for the LLM.
	var summary string
	var err error

	if len(filtered) > 10 {
		// Split into two halves, summarize each, then merge.
		mid := len(filtered) / 2
		summary, err = a.summarizeBatch(ctx, filtered[:mid], filtered[mid:])
	} else {
		summary, err = a.summarizeSingle(ctx, filtered)
	}

	if err != nil {
		summary = fallbackSummary(filtered)
	}

	if a.summary != "" {
		// Can the concatenated summary exceed the context limit? In theory yes,
		// but in practice it's self-correcting: isOverBudget() includes the
		// summary in its token calculation, so a longer summary tightens the
		// budget, which causes the next summarizeOldTurns call to compress
		// history more aggressively. In the extreme case where the summary
		// alone exceeds the budget, forceCompress kicks in as a final safety net.
		summary = a.summary + "\n" + summary
	}
	a.summary = summary
	a.history = a.history[keepFrom:]

	return nil
}

// summarizeSingle sends messages to the LLM for summarization. If the
// request is too long (prompt-too-long error), it retries by dropping the
// oldest 20% of messages, up to 3 times.
func (a *Agent) summarizeSingle(ctx context.Context, msgs []provider.Message) (string, error) {
	const maxRetries = 3

	prompt := summarizePrompt
	if a.summary != "" {
		prompt += "\n\nExisting summary of earlier conversation:\n" + a.summary
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		text := formatForSummary(msgs)
		reply, err := provider.ChatSync(ctx, a.provider, []provider.Message{
			{Role: "system", Content: prompt},
			{Role: "user", Content: "CONVERSATION:\n" + text},
		}, nil) // nil tools, so model can't call any

		if err == nil {
			return reply.Content, nil
		}

		// If this isn't a context-length error, don't retry.
		if !isContextLengthError(err) {
			return "", err
		}

		// Drop the oldest 20% and retry.
		drop := len(msgs) / 5
		if drop < 1 {
			drop = 1
		}
		msgs = msgs[drop:]
		if len(msgs) == 0 {
			return "", fmt.Errorf("no messages left after prompt-too-long retries")
		}
	}

	return "", fmt.Errorf("summarization failed after %d retries", maxRetries)
}

func (a *Agent) summarizeBatch(ctx context.Context, part1, part2 []provider.Message) (string, error) {
	sum1, err := a.summarizeSingle(ctx, part1)
	if err != nil {
		return "", err
	}
	sum2, err := a.summarizeSingle(ctx, part2)
	if err != nil {
		return "", err
	}

	reply, err := provider.ChatSync(ctx, a.provider, []provider.Message{
		{Role: "system", Content: mergePrompt},
		{Role: "user", Content: fmt.Sprintf("Summary 1:\n%s\n\nSummary 2:\n%s", sum1, sum2)},
	}, nil)
	if err != nil {
		// Merge failed, just concatenate.
		return sum1 + "\n" + sum2, nil
	}
	return reply.Content, nil
}

func formatForSummary(msgs []provider.Message) string {
	var sb strings.Builder
	for _, m := range msgs {
		sb.WriteString(m.Role)
		sb.WriteString(": ")
		sb.WriteString(m.Content)
		sb.WriteString("\n")
	}
	return sb.String()
}

// isContextLengthError checks if an error is a context-length / prompt-too-long
// error. Matches against known error strings from both providers:
//
// OpenAI:    "context_length_exceeded" (error code in 400 response)
// Anthropic: "input length and max_tokens exceed context limit" (error message)
//
// Also includes common patterns from other OpenAI-compatible providers.
func isContextLengthError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "context_length_exceeded") ||
		strings.Contains(msg, "context_window_exceeded") ||
		strings.Contains(msg, "maximum context length") ||
		strings.Contains(msg, "exceed context limit") ||
		strings.Contains(msg, "token limit") ||
		strings.Contains(msg, "too many tokens") ||
		strings.Contains(msg, "prompt is too long") ||
		strings.Contains(msg, "request too large")
}

// fallbackSummary creates a crude summary when LLM summarization fails.
// Takes the first 10% of each message (min 200 chars) and joins with " | ".
func fallbackSummary(msgs []provider.Message) string {
	var parts []string
	for _, m := range msgs {
		content := m.Content
		limit := len(content) / 10
		if limit < 200 {
			limit = 200
		}
		if limit < len(content) {
			content = content[:limit] + "..."
		}
		parts = append(parts, m.Role+": "+content)
	}
	return "Conversation summary: " + strings.Join(parts, " | ")
}
