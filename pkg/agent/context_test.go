package agent

import (
	"iclaw/pkg/provider"
	"testing"
)

// ---------------------------------------------------------------------------
// Token estimation tests
// ---------------------------------------------------------------------------

// TestEstimateMessageTokens_EmptyMessage verifies that an empty message
// still has a non-zero token count due to the role string ("user" = 4 chars)
// and the 12-byte fixed overhead (JSON wrapping).
func TestEstimateMessageTokens_EmptyMessage(t *testing.T) {
	msg := provider.Message{Role: "user", Content: ""}
	got := estimateMessageTokens(msg)
	// chars = 4 (role) + 0 (content) = 4
	// tokens = 4*2/5 + 12 = 1 + 12 = 13
	want := 4*2/5 + 12
	if got != want {
		t.Errorf("empty message: got %d tokens, want %d", got, want)
	}
}

// TestEstimateMessageTokens_TextOnly verifies the basic character-to-token
// formula: tokens = (role + content chars)*2/5 + 12 overhead.
func TestEstimateMessageTokens_TextOnly(t *testing.T) {
	msg := provider.Message{
		Role:    "user",                                               // 4 chars
		Content: "aaaaaaaaaabbbbbbbbbbccccccccccddddddddddeeeeeeeeee", // 50 chars
	}
	got := estimateMessageTokens(msg)
	// chars = 4 + 50 = 54
	// tokens = 54*2/5 + 12 = 21 + 12 = 33
	want := 54*2/5 + 12
	if got != want {
		t.Errorf("50-char message: got %d tokens, want %d", got, want)
	}
}

// TestEstimateMessageTokens_WithToolCalls verifies that ToolCall metadata
// (ID, function name, arguments) is included in the token estimate.
// Without this, assistant messages containing tool calls would be
// severely undercounted, causing the budget check to think there's
// more room than there actually is.
func TestEstimateMessageTokens_WithToolCalls(t *testing.T) {
	msg := provider.Message{
		Role:    "assistant", // 9 chars
		Content: "",
		ToolCalls: []provider.ToolCall{
			{
				ID:   "call_abc123", // 11 chars
				Type: "function",
				Function: provider.ToolCallFunc{
					Name:      "get_current_time",   // 16 chars
					Arguments: `{"expression":"1"}`, // 18 chars
				},
			},
		},
	}
	// chars = 9 (role) + 0 (content) + 11 (id) + 16 (name) + 18 (args) = 54
	// tokens = 54*2/5 + 12 = 21 + 12 = 33
	got := estimateMessageTokens(msg)
	want := 54*2/5 + 12
	if got != want {
		t.Errorf("message with tool call: got %d tokens, want %d", got, want)
	}
}

// TestEstimateMessageTokens_ToolResult verifies that a tool result message
// (role="tool") correctly counts Role, Content, and ToolCallID.
func TestEstimateMessageTokens_ToolResult(t *testing.T) {
	msg := provider.Message{
		Role:       "tool",                 // 4 chars
		Content:    "2026-04-14T12:00:00Z", // 20 chars
		ToolCallID: "call_abc123",          // 11 chars
	}
	// chars = 4 + 20 + 11 = 35
	// tokens = 35*2/5 + 12 = 14 + 12 = 26
	got := estimateMessageTokens(msg)
	want := 35*2/5 + 12
	if got != want {
		t.Errorf("tool result message: got %d tokens, want %d", got, want)
	}
}

// TestEstimateTokens_MultipleMessages verifies that estimateTokens sums
// individual message estimates correctly.
func TestEstimateTokens_MultipleMessages(t *testing.T) {
	msgs := []provider.Message{
		{Role: "user", Content: "hello"},      // (4+5)*2/5+12 = 3+12 = 15
		{Role: "assistant", Content: "world"}, // (9+5)*2/5+12 = 5+12 = 17
	}
	got := estimateTokens(msgs)
	want := (9*2/5 + 12) + (14*2/5 + 12)
	if got != want {
		t.Errorf("two messages: got %d tokens, want %d", got, want)
	}
}

// TestEstimateTokens_EmptySlice verifies that an empty message slice
// returns 0 tokens.
func TestEstimateTokens_EmptySlice(t *testing.T) {
	got := estimateTokens(nil)
	if got != 0 {
		t.Errorf("empty slice: got %d tokens, want 0", got)
	}
}

// TestEstimateToolTokens_WithTools verifies that tool definition token
// estimation accounts for name, description, and JSON-serialized
// parameters. This is important because tool definitions are sent with
// every Chat call and can consume significant context budget.
func TestEstimateToolTokens_WithTools(t *testing.T) {
	tools := []provider.Tool{
		{
			Type: "function",
			Function: provider.ToolFunction{
				Name:        "test_tool",
				Description: "A test tool",
				Parameters: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
		},
	}
	got := estimateToolTokens(tools)
	if got <= 20 {
		t.Errorf("tool tokens: got %d, expected > 20 (at least overhead)", got)
	}
}

// TestEstimateToolTokens_Empty verifies that no tools = 0 tokens.
func TestEstimateToolTokens_Empty(t *testing.T) {
	got := estimateToolTokens(nil)
	if got != 0 {
		t.Errorf("empty tools: got %d tokens, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// Turn boundary tests
// ---------------------------------------------------------------------------

// TestParseTurnBoundaries_SimpleConversation verifies that boundaries are
// correctly identified in a normal alternating user/assistant conversation.
// Each user message starts a new Turn, so boundaries should be at every
// even index: [0, 2, 4].
func TestParseTurnBoundaries_SimpleConversation(t *testing.T) {
	history := []provider.Message{
		{Role: "user", Content: "hello"}, // Turn 1 start
		{Role: "assistant", Content: "hi"},
		{Role: "user", Content: "how are you"}, // Turn 2 start
		{Role: "assistant", Content: "good"},
		{Role: "user", Content: "bye"}, // Turn 3 start
		{Role: "assistant", Content: "goodbye"},
	}
	got := parseTurnBoundaries(history)
	want := []int{0, 2, 4}
	if len(got) != len(want) {
		t.Fatalf("boundaries: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("boundary[%d]: got %d, want %d", i, got[i], want[i])
		}
	}
}

// TestParseTurnBoundaries_WithToolCalls verifies that tool_call and
// tool_result messages do NOT create new boundaries. They belong to the
// same Turn as the preceding user message. Only user messages start Turns.
func TestParseTurnBoundaries_WithToolCalls(t *testing.T) {
	history := []provider.Message{
		{Role: "user", Content: "what time is it"},                                  // [0] Turn 1
		{Role: "assistant", Content: "", ToolCalls: []provider.ToolCall{{ID: "1"}}}, // [1]
		{Role: "tool", Content: "12:00", ToolCallID: "1"},                           // [2]
		{Role: "assistant", Content: "it's noon"},                                   // [3]
		{Role: "user", Content: "thanks"},                                           // [4] Turn 2
		{Role: "assistant", Content: "you're welcome"},                              // [5]
	}
	got := parseTurnBoundaries(history)
	want := []int{0, 4}
	if len(got) != len(want) {
		t.Fatalf("boundaries: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("boundary[%d]: got %d, want %d", i, got[i], want[i])
		}
	}
}

// TestParseTurnBoundaries_Empty verifies that an empty history produces
// no boundaries.
func TestParseTurnBoundaries_Empty(t *testing.T) {
	got := parseTurnBoundaries(nil)
	if len(got) != 0 {
		t.Errorf("empty history: got %v, want empty", got)
	}
}

// TestParseTurnBoundaries_SingleUser verifies a history with just one
// user message produces exactly one boundary.
func TestParseTurnBoundaries_SingleUser(t *testing.T) {
	history := []provider.Message{
		{Role: "user", Content: "hello"},
	}
	got := parseTurnBoundaries(history)
	if len(got) != 1 || got[0] != 0 {
		t.Errorf("single user: got %v, want [0]", got)
	}
}

// TestFindSafeBoundary_ExactMatch verifies that when targetIndex falls
// exactly on a Turn boundary, that boundary is returned.
func TestFindSafeBoundary_ExactMatch(t *testing.T) {
	history := []provider.Message{
		{Role: "user", Content: "a"}, // boundary 0
		{Role: "assistant", Content: "b"},
		{Role: "user", Content: "c"}, // boundary 2
		{Role: "assistant", Content: "d"},
	}
	got := findSafeBoundary(history, 2)
	if got != 2 {
		t.Errorf("exact match: got %d, want 2", got)
	}
}

// TestFindSafeBoundary_BetweenBoundaries verifies that when targetIndex
// falls between two boundaries, the earlier one (at or before target) is
// returned. This preserves more recent messages.
func TestFindSafeBoundary_BetweenBoundaries(t *testing.T) {
	history := []provider.Message{
		{Role: "user", Content: "a"}, // boundary 0
		{Role: "assistant", Content: "", ToolCalls: []provider.ToolCall{{ID: "1"}}}, // 1
		{Role: "tool", Content: "result", ToolCallID: "1"},                          // 2
		{Role: "assistant", Content: "done"},                                        // 3
		{Role: "user", Content: "next"},                                             // boundary 4
		{Role: "assistant", Content: "ok"},                                          // 5
	}
	// targetIndex 3 is between boundary 0 and boundary 4 → should return 0
	got := findSafeBoundary(history, 3)
	if got != 0 {
		t.Errorf("between boundaries: got %d, want 0", got)
	}

	// targetIndex 5 is after boundary 4 → should return 4
	got = findSafeBoundary(history, 5)
	if got != 4 {
		t.Errorf("after last boundary: got %d, want 4", got)
	}
}

// TestFindSafeBoundary_EmptyHistory verifies that an empty history
// returns 0 (no boundaries exist, nothing to cut).
func TestFindSafeBoundary_EmptyHistory(t *testing.T) {
	got := findSafeBoundary(nil, 0)
	if got != 0 {
		t.Errorf("empty history: got %d, want 0", got)
	}
}

// TestFindSafeBoundary_FallbackToAfter verifies the defensive edge case:
// if no boundary exists at or before targetIndex (e.g. history starts with
// non-user messages), the function falls back to the first boundary AFTER
// targetIndex. This shouldn't happen in normal conversations but prevents
// a panic if history is in an unusual state.
func TestFindSafeBoundary_FallbackToAfter(t *testing.T) {
	history := []provider.Message{
		{Role: "assistant", Content: "stale"},  // 0 — not a boundary
		{Role: "assistant", Content: "stale2"}, // 1 — not a boundary
		{Role: "user", Content: "hello"},       // 2 — first boundary
		{Role: "assistant", Content: "hi"},     // 3
	}
	// targetIndex 0, no boundary at or before 0 → fallback to first boundary = 2
	got := findSafeBoundary(history, 0)
	if got != 2 {
		t.Errorf("fallback to after: got %d, want 2", got)
	}
}
