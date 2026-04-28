package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"iclaw/pkg/provider"
	"iclaw/pkg/tool"
)

// fakeTool implements tool.Tool for testing.
type fakeTool struct {
	name          string
	needsApproval bool
	executeFn     func(args string) (string, error)
}

func (f *fakeTool) Name() string        { return f.name }
func (f *fakeTool) Description() string { return "test tool" }
func (f *fakeTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (f *fakeTool) Execute(args string) (string, error) { return f.executeFn(args) }
func (f *fakeTool) Approval() bool                      { return f.needsApproval }
func (f *fakeTool) Workspace() bool                     { return false }

// fakeResponse is one entry in a fakeProvider's scripted responses. Each
// call to Chat consumes one response: a non-nil err means that the call fails,
// otherwise the reply is returned.
type fakeResponse struct {
	reply provider.Message
	err   error
}

// fakeProvider is an in-memory Provider implementation used by the tests in
// this file. It performs no network I/O. Instead, it returns answers from a
// pre-queued script of responses and records the messages it received on
// the most recent call.
//
// The script lets a single test cover multi-turn conversations: each Chat
// call pops the next response in order. Calling Chat more times than the
// script has entry panics — that's a test bug we want to surface loudly,
// not silently mask by returning a zero value.
type fakeProvider struct {
	gotMsgs   []provider.Message // msgs from the MOST RECENT Chat call
	responses []fakeResponse     // scripted responses, consumed in order
	calls     int                // number of Chat calls so far
}

func (f *fakeProvider) Chat(_ context.Context, msgs []provider.Message, _ []provider.Tool) (provider.Message, error) {
	f.gotMsgs = msgs
	if f.calls >= len(f.responses) {
		panic(fmt.Sprintf("fakeProvider: Chat called %d times, only %d responses queued", f.calls+1, len(f.responses)))
	}
	fr := f.responses[f.calls]
	f.calls++
	return fr.reply, fr.err
}

// TestAgentReply covers the happy path of Agent.Reply when a non-empty
// system prompt is configured. It pins down three behaviors that together
// describe the full contract of Reply in this branch:
//
//  1. Reply must return whatever the underlying Provider returned, verbatim.
//     The Agent is a transparent pass-through for the model output — it
//     must not truncate, rewrap, or otherwise mutate the reply.
//
//  2. Reply must hand the Provider exactly two messages: one system and
//     one user. Any other count means the Agent either dropped the system
//     prompt or injected something it shouldn't have.
//
//  3. The two messages must have the correct role, content, AND order.
//     The system message must come first; the OpenAI Chat Completions
//     protocol expects system instructions at the head of the message
//     array, and putting them anywhere else can confuse the model.
func TestAgentReply(t *testing.T) {
	const (
		system = "you are a helpful assistant"
		user   = "hello"
		reply  = "hi there"
	)
	fp := &fakeProvider{
		responses: []fakeResponse{
			{reply: provider.Message{Role: "assistant", Content: reply}},
		},
	}
	a := New(fp, system, nil, 0)

	// (1) The Provider's reply should propagate through Agent unchanged.
	got, err := a.Reply(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	if got != reply {
		t.Errorf("reply: got %q, want %q", got, reply)
	}

	// (2) Exactly two messages should have been sent down to the Provider (the system prompt and the user input).
	if len(fp.gotMsgs) != 2 {
		t.Fatalf("msgs len: got %d, want 2", len(fp.gotMsgs))
	}
	// (3a) The first message must be the system prompt, with the exact
	// content the Agent was constructed with.
	if fp.gotMsgs[0].Role != "system" || fp.gotMsgs[0].Content != system {
		t.Errorf("msgs[0]: got %+v, want system/%q", fp.gotMsgs[0], system)
	}
	// (3b) The second message must be the user input passed to Reply.
	if fp.gotMsgs[1].Role != "user" || fp.gotMsgs[1].Content != user {
		t.Errorf("msgs[1]: got %+v, want user/%q", fp.gotMsgs[1], user)
	}
}

// TestAgentReply_EmptySystem covers the edge case where Agent is built with
// an empty system prompt. The contract here is that Agent must NOT emit a
// `{role:"system", content:""}` message in that case — empty system
// messages are garbage data that some models react to in surprising ways,
// so Agent.Reply has an explicit `if a.system != ""` guard around the
// system-message appended.
//
// This test exists specifically to lock that guard in place: if a future
// refactor removes the `if`, this test will fail because gotMsgs would
// contain two messages instead of one.
func TestAgentReply_EmptySystem(t *testing.T) {
	fp := &fakeProvider{responses: []fakeResponse{{reply: provider.Message{Role: "assistant", Content: "ok"}}}}
	a := New(fp, "", nil, 0)

	if _, err := a.Reply(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}

	// Only the user message should reach the Provider — no empty system.
	if len(fp.gotMsgs) != 1 {
		t.Fatalf("msgs len: got %d, want 1 (no system message when system is empty)", len(fp.gotMsgs))
	}
	if fp.gotMsgs[0].Role != "user" || fp.gotMsgs[0].Content != "hello" {
		t.Errorf("msgs[0]: got %+v, want user/hello", fp.gotMsgs[0])
	}
}

// TestAgentReply_RemembersHistory verifies the core promise of Step 2:
// Agent accumulates conversation history across Reply calls and replays it
// on every subsequent call, so the model "remembers" earlier turns.
//
// The fake provider doesn't actually remember anything — what we're
// pinning down is that on the SECOND call, msgs contains the entire prior
// conversation (system + every previous user/assistant pair) followed by
// the new user message. A real LLM with that input would be able to
// answer "what is my name?" correctly.
//
// If a future refactor breaks the history (e.g. someone wires up a new
// per-call slice instead of appending to a.history, or forgets to record
// the assistant reply), this test will fail because the second call's
// msgs will be too short or miss the prior turns.
func TestAgentReply_RemembersHistory(t *testing.T) {
	const system = "you are a helpful assistant"
	fp := &fakeProvider{
		responses: []fakeResponse{
			{reply: provider.Message{
				Role:    "assistant",
				Content: "nice to meet you, xiaoming",
			}}, // turn 1 reply
			{reply: provider.Message{
				Role:    "assistant",
				Content: "your name is xiaoming",
			}}, // turn 2 reply
		},
	}
	a := New(fp, system, nil, 0)
	ctx := context.Background()

	// Turn 1: introduce a name. We don't assert on this call's msgs;
	// TestAgentReply already covers the single-call shape.
	if _, err := a.Reply(ctx, "my name is xiaoming"); err != nil {
		t.Fatal(err)
	}

	// Turn 2: ask about the name.
	got, err := a.Reply(ctx, "what is my name?")
	if err != nil {
		t.Fatal(err)
	}
	if got != "your name is xiaoming" {
		t.Errorf("reply: got %q, want %q", got, "your name is xiaoming")
	}

	// On the second Chat call, msgs must contain:
	//   [system, user(turn1), assistant(turn1), user(turn2)]
	// i.e. system prompt + the entire accumulated history with the new
	// user message at the end.
	want := []provider.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: "my name is xiaoming"},
		{Role: "assistant", Content: "nice to meet you, xiaoming"},
		{Role: "user", Content: "what is my name?"},
	}
	if len(fp.gotMsgs) != len(want) {
		t.Fatalf("msgs len on second call: got %d, want %d", len(fp.gotMsgs), len(want))
	}
	for i := range want {
		if fp.gotMsgs[i].Role != want[i].Role || fp.gotMsgs[i].Content != want[i].Content {
			t.Errorf("msgs[%d]: got %+v, want %+v", i, fp.gotMsgs[i], want[i])
		}
	}
}

// TestAgentReply_RollsBackOnError pins down the failure-recovery contract:
// when the Provider returns an error, Agent must NOT leave the new user
// message stranded in history. Otherwise, on the next successful call,
// msgs would contain two consecutive user messages with no assistant in
// between — an illegal conversation shape that confuses many models.
//
// The harder scenario this test covers is the one where history is
// non-empty BEFORE the failing call: if rollback uses the wrong index or
// removes the wrong element, it could silently corrupt earlier turns
// instead of just removing the new user. So we run a successful turn 1
// first, then make turn 2 fail, then assert that history is exactly what
// it was after turn 1 — the same length, same contents.
func TestAgentReply_RollsBackOnError(t *testing.T) {
	const system = "you are a helpful assistant"
	boom := errors.New("provider boom")
	fp := &fakeProvider{
		responses: []fakeResponse{
			{reply: provider.Message{
				Role:    "assistant",
				Content: "ok",
			}}, // turn 1: success
			{err: boom}, // turn 2: failure
		},
	}
	a := New(fp, system, nil, 0)
	ctx := context.Background()

	// Turn 1: succeed so history has a real (user, assistant) pair.
	if _, err := a.Reply(ctx, "first"); err != nil {
		t.Fatal(err)
	}
	if len(a.history) != 2 {
		t.Fatalf("after turn 1: history len = %d, want 2", len(a.history))
	}

	// Turn 2: provider fails. Reply must propagate the error AND roll
	// back the user message it speculatively appended at the start of
	// the call, leaving history exactly as it was after turn 1.
	_, err := a.Reply(ctx, "second")
	if !errors.Is(err, boom) {
		t.Fatalf("err: got %v, want %v", err, boom)
	}
	if len(a.history) != 2 {
		t.Fatalf("after turn 2 failure: history len = %d, want 2 (user message should have been rolled back)", len(a.history))
	}
	// Most importantly: turn 1's content must still be intact. A buggy
	// rollback could remove the wrong element and silently corrupt the
	// prior turn — this assertion catches that.
	if a.history[0].Content != "first" || a.history[1].Content != "ok" {
		t.Errorf("history corrupted after rollback: %+v", a.history)
	}
}

// TestApproval_Denied verifies that when OnApproval returns false, the tool
// is NOT executed and the model receives a rejection message. This is the
// core safety guarantee: if the user says no, nothing happens.
func TestApproval_Denied(t *testing.T) {
	// Set up a tool that needs approval and tracks if Execute was called.
	executed := false
	reg := tool.NewRegistry()
	reg.Register(&fakeTool{
		name:          "dangerous_tool",
		needsApproval: true,
		executeFn: func(args string) (string, error) {
			executed = true
			return "executed", nil
		},
	})

	// fakeProvider: first response triggers tool call, second is final reply.
	fp := &fakeProvider{
		responses: []fakeResponse{
			{reply: provider.Message{
				Role: "assistant",
				ToolCalls: []provider.ToolCall{{
					ID:       "call_1",
					Type:     "function",
					Function: provider.ToolCallFunc{Name: "dangerous_tool", Arguments: "{}"},
				}},
			}},
			{reply: provider.Message{Role: "assistant", Content: "ok, I won't do that"}},
		},
	}

	a := New(fp, "", reg, 0)
	a.OnApproval = func(toolName string, args string) bool {
		return false // deny
	}

	got, err := a.Reply(context.Background(), "do something dangerous")
	if err != nil {
		t.Fatal(err)
	}
	if got != "ok, I won't do that" {
		t.Errorf("reply: got %q", got)
	}
	if executed {
		t.Error("tool should NOT have been executed after denial")
	}

	// The tool result in history should contain the rejection message.
	found := false
	for _, m := range a.history {
		if m.Role == "tool" && strings.Contains(m.Content, "User rejected") {
			found = true
		}
	}
	if !found {
		t.Error("history should contain a 'User rejected' tool result")
	}
}

// TestApproval_Approved verifies that when OnApproval returns true, the tool
// executes normally and its real result is returned to the model.
func TestApproval_Approved(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(&fakeTool{
		name:          "dangerous_tool",
		needsApproval: true,
		executeFn: func(args string) (string, error) {
			return "tool executed successfully", nil
		},
	})

	fp := &fakeProvider{
		responses: []fakeResponse{
			{reply: provider.Message{
				Role: "assistant",
				ToolCalls: []provider.ToolCall{{
					ID:       "call_1",
					Type:     "function",
					Function: provider.ToolCallFunc{Name: "dangerous_tool", Arguments: "{}"},
				}},
			}},
			{reply: provider.Message{Role: "assistant", Content: "done"}},
		},
	}

	a := New(fp, "", reg, 0)
	a.OnApproval = func(toolName string, args string) bool {
		return true // approve
	}

	got, err := a.Reply(context.Background(), "do something")
	if err != nil {
		t.Fatal(err)
	}
	if got != "done" {
		t.Errorf("reply: got %q, want %q", got, "done")
	}

	// The tool result should contain the real output, not a rejection.
	found := false
	for _, m := range a.history {
		if m.Role == "tool" && strings.Contains(m.Content, "tool executed successfully") {
			found = true
		}
	}
	if !found {
		t.Error("history should contain the real tool result")
	}
}

// TestApproval_NilCallback verifies that when OnApproval is nil (not set),
// tools with NeedsApproval=true are auto-approved. This ensures backwards
// compatibility: existing code that doesn't set OnApproval still works.
func TestApproval_NilCallback(t *testing.T) {
	executed := false
	reg := tool.NewRegistry()
	reg.Register(&fakeTool{
		name:          "dangerous_tool",
		needsApproval: true,
		executeFn: func(args string) (string, error) {
			executed = true
			return "executed", nil
		},
	})

	fp := &fakeProvider{
		responses: []fakeResponse{
			{reply: provider.Message{
				Role: "assistant",
				ToolCalls: []provider.ToolCall{{
					ID:       "call_1",
					Type:     "function",
					Function: provider.ToolCallFunc{Name: "dangerous_tool", Arguments: "{}"},
				}},
			}},
			{reply: provider.Message{Role: "assistant", Content: "done"}},
		},
	}

	a := New(fp, "", reg, 0)
	// OnApproval is nil (not set)

	_, err := a.Reply(context.Background(), "do something")
	if err != nil {
		t.Fatal(err)
	}
	if !executed {
		t.Error("tool should be auto-approved when OnApproval is nil")
	}
}
