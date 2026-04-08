package agent

import (
	"context"
	"testing"

	"iclaw/pkg/provider"
)

// fakeProvider is an in-memory Provider implementation used by the tests in
// this file. It performs no network I/O: every Chat call simply records the
// messages it received into gotMsgs and returns the canned reply string.
type fakeProvider struct {
	gotMsgs []provider.Message
	reply   string
}

func (f *fakeProvider) Chat(_ context.Context, msgs []provider.Message) (string, error) {
	f.gotMsgs = msgs
	return f.reply, nil
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
//     one user. Any other count means Agent either dropped the system
//     prompt or injected something it shouldn't have.
//
//  3. The two messages must have the correct role, content, AND order.
//     The system message must come first; the OpenAI Chat Completions
//     protocol expects system instructions at the head of the messages
//     array, and putting them anywhere else can confuse the model.
func TestAgentReply(t *testing.T) {
	const (
		system = "you are a helpful assistant"
		user   = "hello"
		reply  = "hi there"
	)
	fp := &fakeProvider{reply: reply}
	a := New(fp, system)

	// (1) The Provider's reply should propagate through Agent unchanged.
	got, err := a.Reply(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	if got != reply {
		t.Errorf("reply: got %q, want %q", got, reply)
	}

	// (2) Exactly two messages should have been sent down to the Provider.
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
// system-message append.
//
// This test exists specifically to lock that guard in place: if a future
// refactor removes the `if`, this test will fail because gotMsgs would
// contain two messages instead of one.
func TestAgentReply_EmptySystem(t *testing.T) {
	fp := &fakeProvider{reply: "ok"}
	a := New(fp, "")

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
