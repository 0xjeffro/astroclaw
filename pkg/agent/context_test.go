package agent

import (
	"context"
	"errors"
	"iclaw/pkg/provider"
	"strings"
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

// ---------------------------------------------------------------------------
// Budget check tests
// ---------------------------------------------------------------------------

// TestIsOverBudget_NoLimit verifies that when contextWindow is 0 (no limit),
// isOverBudget always returns false regardless of history size. This ensures
// that agents created without a context window (e.g. in tests) never trigger
// compression.
func TestIsOverBudget_NoLimit(t *testing.T) {
	fp := &fakeProvider{}
	a := New(fp, "system prompt", nil, 0)
	// Add a bunch of history — should still not be over budget.
	for i := 0; i < 100; i++ {
		a.history = append(a.history, provider.Message{Role: "user", Content: "hello world this is a long message"})
	}
	if a.isOverBudget() {
		t.Error("contextWindow=0 should never be over budget")
	}
}

// TestIsOverBudget_UnderBudget verifies that a small history within a
// generous context window returns false.
func TestIsOverBudget_UnderBudget(t *testing.T) {
	fp := &fakeProvider{}
	a := New(fp, "short system", nil, 128000) // 128K window
	a.history = []provider.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	}
	if a.isOverBudget() {
		t.Error("small history in 128K window should not be over budget")
	}
}

// TestIsOverBudget_OverBudget verifies that a large history exceeding a
// small context window returns true. We use a tiny window (500 tokens) and
// fill history with enough messages to exceed it.
func TestIsOverBudget_OverBudget(t *testing.T) {
	fp := &fakeProvider{}
	a := New(fp, "system", nil, 500) // tiny 500-token window
	// Each message ≈ 14 tokens. 500 - 4096 (maxResponseTokens) is already
	// negative, so ANY history should be over budget with this tiny window.
	a.history = []provider.Message{
		{Role: "user", Content: "hello"},
	}
	if !a.isOverBudget() {
		t.Error("history in 500-token window should be over budget (maxResponseTokens alone exceeds it)")
	}
}

// TestIsOverBudget_SummaryCountedInBudget verifies that the summary field
// is included in the budget calculation. A large summary should push the
// total over a tight budget.
func TestIsOverBudget_SummaryCountedInBudget(t *testing.T) {
	fp := &fakeProvider{}
	a := New(fp, "", nil, 5000)
	a.history = []provider.Message{
		{Role: "user", Content: "hi"},
	}
	// Without summary: should be under budget.
	if a.isOverBudget() {
		t.Fatal("should be under budget without summary")
	}
	// Add a huge summary that pushes it over.
	a.summary = string(make([]byte, 10000)) // 10K chars ≈ 4000 tokens
	if !a.isOverBudget() {
		t.Error("large summary should push total over 5000-token budget")
	}
}

// ---------------------------------------------------------------------------
// Force compression tests
// ---------------------------------------------------------------------------

// TestForceCompress_DropsOldestHalf verifies that forceCompress cuts at the
// midpoint Turn boundary, keeping roughly the newest 50% of Turns and
// dropping the oldest 50%.
func TestForceCompress_DropsOldestHalf(t *testing.T) {
	fp := &fakeProvider{}
	a := New(fp, "", nil, 0)
	a.history = []provider.Message{
		{Role: "user", Content: "turn1"}, // Turn 1 — boundary 0
		{Role: "assistant", Content: "reply1"},
		{Role: "user", Content: "turn2"}, // Turn 2 — boundary 2
		{Role: "assistant", Content: "reply2"},
		{Role: "user", Content: "turn3"}, // Turn 3 — boundary 4
		{Role: "assistant", Content: "reply3"},
	}
	// boundaries = [0, 2, 4], midpoint = boundaries[3/2] = boundaries[1] = 2
	a.forceCompress()

	if len(a.history) != 4 {
		t.Fatalf("history len: got %d, want 4", len(a.history))
	}
	if a.history[0].Content != "turn2" {
		t.Errorf("first message after compress: got %q, want %q", a.history[0].Content, "turn2")
	}
}

// TestForceCompress_PreservesToolCallPairs verifies that truncation at Turn
// boundaries keeps tool_call/tool_result pairs intact. If we cut in the
// middle of a tool chain, the model would see orphaned messages.
func TestForceCompress_PreservesToolCallPairs(t *testing.T) {
	fp := &fakeProvider{}
	a := New(fp, "", nil, 0)
	a.history = []provider.Message{
		{Role: "user", Content: "turn1"}, // Turn 1
		{Role: "assistant", Content: "", ToolCalls: []provider.ToolCall{{ID: "1"}}},
		{Role: "tool", Content: "result1", ToolCallID: "1"},
		{Role: "assistant", Content: "done1"},
		{Role: "user", Content: "turn2"}, // Turn 2
		{Role: "assistant", Content: "reply2"},
		{Role: "user", Content: "turn3"}, // Turn 3
		{Role: "assistant", Content: "reply3"},
	}
	// boundaries = [0, 4, 6], midpoint = boundaries[1] = 4
	a.forceCompress()

	// Turn 1 (with tool chain) should be fully dropped, Turn 2+3 kept.
	if len(a.history) != 4 {
		t.Fatalf("history len: got %d, want 4", len(a.history))
	}
	if a.history[0].Content != "turn2" {
		t.Errorf("first message: got %q, want %q", a.history[0].Content, "turn2")
	}
}

// TestForceCompress_SetsSummaryNote verifies that forceCompress records
// how many messages were dropped, so the model knows context was lost.
func TestForceCompress_SetsSummaryNote(t *testing.T) {
	fp := &fakeProvider{}
	a := New(fp, "", nil, 0)
	a.history = []provider.Message{
		{Role: "user", Content: "a"},
		{Role: "assistant", Content: "b"},
		{Role: "user", Content: "c"},
		{Role: "assistant", Content: "d"},
	}
	a.forceCompress()

	if a.summary == "" {
		t.Fatal("summary should contain truncation note")
	}
	if !strings.Contains(a.summary, "dropped 2 oldest messages") {
		t.Errorf("summary %q should mention dropped count", a.summary)
	}
}

// TestForceCompress_AppendToExistingSummary verifies that if a summary
// already exists (from a prior summarization), the truncation note is
// appended rather than replacing it.
func TestForceCompress_AppendToExistingSummary(t *testing.T) {
	fp := &fakeProvider{}
	a := New(fp, "", nil, 0)
	a.summary = "user's name is xiaoming"
	a.history = []provider.Message{
		{Role: "user", Content: "a"},
		{Role: "assistant", Content: "b"},
		{Role: "user", Content: "c"},
		{Role: "assistant", Content: "d"},
	}
	a.forceCompress()

	if !strings.Contains(a.summary, "user's name is xiaoming") {
		t.Error("original summary should be preserved")
	}
	if !strings.Contains(a.summary, "Emergency compression") {
		t.Error("truncation note should be appended")
	}
}

// TestForceCompress_TooShortToCompress verifies that history with ≤2
// messages is left untouched — cutting it would make things worse.
func TestForceCompress_TooShortToCompress(t *testing.T) {
	fp := &fakeProvider{}
	a := New(fp, "", nil, 0)
	a.history = []provider.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}
	a.forceCompress()

	if len(a.history) != 2 {
		t.Errorf("history should be unchanged: got %d messages, want 2", len(a.history))
	}
}

// TestForceCompress_SingleTurn verifies the edge case where history has
// only one Turn (one user message + many tool calls). cutIndex would be 0,
// so forceCompress returns without modifying history (can't cut before the
// only user message).
func TestForceCompress_SingleTurn(t *testing.T) {
	fp := &fakeProvider{}
	a := New(fp, "", nil, 0)
	a.history = []provider.Message{
		{Role: "user", Content: "do something"},
		{Role: "assistant", Content: "", ToolCalls: []provider.ToolCall{{ID: "1"}}},
		{Role: "tool", Content: "result", ToolCallID: "1"},
		{Role: "assistant", Content: "done"},
	}
	originalLen := len(a.history)
	a.forceCompress()

	if len(a.history) != originalLen {
		t.Errorf("single Turn: history should be unchanged, got %d want %d", len(a.history), originalLen)
	}
}

// ---------------------------------------------------------------------------
// LLM summarization tests
// ---------------------------------------------------------------------------

// TestSummarizeOldTurns_Basic verifies the happy path: LLM returns a
// summary, oldest Turns are removed, last 2 Turns are kept intact.
func TestSummarizeOldTurns_Basic(t *testing.T) {
	fp := &fakeProvider{
		responses: []fakeResponse{
			{reply: provider.Message{Role: "assistant", Content: "User discussed goroutines and fixed a bug."}},
		},
	}
	a := New(fp, "", nil, 128000)
	a.history = []provider.Message{
		{Role: "user", Content: "turn1"},
		{Role: "assistant", Content: "reply1"},
		{Role: "user", Content: "turn2"},
		{Role: "assistant", Content: "reply2"},
		{Role: "user", Content: "turn3"},
		{Role: "assistant", Content: "reply3"},
		{Role: "user", Content: "turn4"},
		{Role: "assistant", Content: "reply4"},
	}

	err := a.summarizeOldTurns(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(a.history) != 4 {
		t.Fatalf("history len: got %d, want 4", len(a.history))
	}
	if a.history[0].Content != "turn3" {
		t.Errorf("first remaining message: got %q, want %q", a.history[0].Content, "turn3")
	}
	if !strings.Contains(a.summary, "goroutines") {
		t.Errorf("summary %q should contain the LLM's response", a.summary)
	}
}

// TestSummarizeOldTurns_IncludesToolResults verifies that small tool
// results are included in the summarization input, not filtered out.
func TestSummarizeOldTurns_IncludesToolResults(t *testing.T) {
	fp := &fakeProvider{
		responses: []fakeResponse{
			{reply: provider.Message{Role: "assistant", Content: "summary with tool context"}},
		},
	}
	a := New(fp, "", nil, 128000)
	a.history = []provider.Message{
		{Role: "user", Content: "what time is it"},
		{Role: "assistant", Content: "", ToolCalls: []provider.ToolCall{{ID: "1"}}},
		{Role: "tool", Content: "2026-04-14T12:00:00Z", ToolCallID: "1"},
		{Role: "assistant", Content: "it's noon"},
		{Role: "user", Content: "turn2"},
		{Role: "assistant", Content: "reply2"},
		{Role: "user", Content: "turn3"},
		{Role: "assistant", Content: "reply3"},
	}

	_ = a.summarizeOldTurns(context.Background())

	if len(fp.gotMsgs) < 2 {
		t.Fatalf("expected at least 2 messages sent to LLM, got %d", len(fp.gotMsgs))
	}
	if !strings.Contains(fp.gotMsgs[1].Content, "2026-04-14T12:00:00Z") {
		t.Errorf("summarization input should include tool result, got: %q", fp.gotMsgs[1].Content)
	}
}

// TestSummarizeOldTurns_TooFewMessages verifies that summarization is
// skipped when history has 4 or fewer messages.
func TestSummarizeOldTurns_TooFewMessages(t *testing.T) {
	fp := &fakeProvider{}
	a := New(fp, "", nil, 128000)
	a.history = []provider.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}

	err := a.summarizeOldTurns(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(a.history) != 2 {
		t.Errorf("history should be unchanged: got %d, want 2", len(a.history))
	}
	if fp.calls != 0 {
		t.Errorf("no LLM calls should be made: got %d", fp.calls)
	}
}

// TestSummarizeOldTurns_LLMFailure verifies that when the LLM fails,
// the fallback summary is used instead. History is still truncated.
func TestSummarizeOldTurns_LLMFailure(t *testing.T) {
	fp := &fakeProvider{
		responses: []fakeResponse{
			{err: errors.New("network error")},
		},
	}
	a := New(fp, "", nil, 128000)
	a.history = []provider.Message{
		{Role: "user", Content: "turn1"},
		{Role: "assistant", Content: "reply1"},
		{Role: "user", Content: "turn2"},
		{Role: "assistant", Content: "reply2"},
		{Role: "user", Content: "turn3"},
		{Role: "assistant", Content: "reply3"},
	}

	err := a.summarizeOldTurns(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(a.summary, "Conversation summary:") {
		t.Errorf("expected fallback summary, got: %q", a.summary)
	}
	if len(a.history) != 4 {
		t.Fatalf("history len: got %d, want 4", len(a.history))
	}
}

// TestSummarizeOldTurns_AppendsToExistingSummary verifies that new
// summaries are appended to existing ones, preserving earlier context.
func TestSummarizeOldTurns_AppendsToExistingSummary(t *testing.T) {
	fp := &fakeProvider{
		responses: []fakeResponse{
			{reply: provider.Message{Role: "assistant", Content: "new summary content"}},
		},
	}
	a := New(fp, "", nil, 128000)
	a.summary = "user's name is xiaoming"
	a.history = []provider.Message{
		{Role: "user", Content: "turn1"},
		{Role: "assistant", Content: "reply1"},
		{Role: "user", Content: "turn2"},
		{Role: "assistant", Content: "reply2"},
		{Role: "user", Content: "turn3"},
		{Role: "assistant", Content: "reply3"},
	}

	_ = a.summarizeOldTurns(context.Background())

	if !strings.Contains(a.summary, "user's name is xiaoming") {
		t.Error("existing summary should be preserved")
	}
	if !strings.Contains(a.summary, "new summary content") {
		t.Error("new summary should be appended")
	}
}

// TestFallbackSummary verifies the crude fallback: each message is
// truncated to 10% (min 200 chars) and joined with " | ".
func TestFallbackSummary(t *testing.T) {
	msgs := []provider.Message{
		{Role: "user", Content: "hello world"},
		{Role: "assistant", Content: "goodbye world"},
	}
	got := fallbackSummary(msgs)

	if !strings.HasPrefix(got, "Conversation summary: ") {
		t.Errorf("should start with 'Conversation summary: ', got: %q", got)
	}
	if !strings.Contains(got, "user: hello world") {
		t.Errorf("should contain user message, got: %q", got)
	}
	if !strings.Contains(got, " | ") {
		t.Errorf("should join with ' | ', got: %q", got)
	}
}

// TestIsContextLengthError verifies detection of context-length errors
// from real API responses. Each test case is based on actual error strings
// from OpenAI, Anthropic, or other OpenAI-compatible providers.
func TestIsContextLengthError(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		// OpenAI: exact error code
		{"openai: status 400: context_length_exceeded", true},
		// Anthropic: exact error message
		{"input length and max_tokens exceed context limit: 150000 + 4096 > 128000", true},
		// OpenAI-compatible providers
		{"context_window_exceeded", true},
		{"This model's maximum context length is 128000 tokens", true},
		{"token limit exceeded", true},
		{"request has too many tokens", true},
		{"prompt is too long", true},
		{"request too large for model", true},
		// Should NOT match
		{"network timeout", false},
		{"unauthorized", false},
		{"max_tokens must be greater than 0", false}, // not a context error
	}
	for _, tt := range tests {
		got := isContextLengthError(errors.New(tt.msg))
		if got != tt.want {
			t.Errorf("isContextLengthError(%q): got %v, want %v", tt.msg, got, tt.want)
		}
	}
}
