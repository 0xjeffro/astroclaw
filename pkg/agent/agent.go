package agent

import (
	"context"
	"iclaw/pkg/provider"
)

type Agent struct {
	provider provider.Provider
	system   string
	history  []provider.Message
}

func New(p provider.Provider, system string) *Agent {
	return &Agent{provider: p, system: system}
}

func (a *Agent) Reply(ctx context.Context, userText string) (string, error) {

	// Add the user message to the history.
	a.history = append(a.history, provider.Message{Role: "user", Content: userText})

	var msgs []provider.Message
	if a.system != "" { // If the system prompt is non-empty, add it to the history.
		msgs = append(msgs, provider.Message{Role: "system", Content: a.system})
	}
	msgs = append(msgs, a.history...) // Add the entire history to the message list (including the userText this time).

	reply, err := a.provider.Chat(ctx, msgs)
	if err != nil {
		// If the provider fails, remove the last message from the history.
		a.history = a.history[:len(a.history)-1]
		return "", err
	} else {
		a.history = append(a.history, provider.Message{Role: "assistant", Content: reply})
	}

	return reply, err
}
