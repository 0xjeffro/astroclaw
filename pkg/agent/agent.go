package agent

import (
	"context"
	"iclaw/pkg/provider"
)

type Agent struct {
	provider provider.Provider
	system   string
}

func New(p provider.Provider, system string) *Agent {
	return &Agent{provider: p, system: system}
}

func (a *Agent) Reply(ctx context.Context, userText string) (string, error) {
	var msgs []provider.Message
	if a.system != "" {
		msgs = append(msgs, provider.Message{Role: "system", Content: a.system})
	}
	msgs = append(msgs, provider.Message{Role: "user", Content: userText})
	return a.provider.Chat(ctx, msgs)
}
