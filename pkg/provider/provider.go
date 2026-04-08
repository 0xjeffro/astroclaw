package provider

import "context"

type Message struct {
	Role    string `json:"role"` // "system" | "user" | "assistant"
	Content string `json:"content"`
}

type Provider interface {
	Chat(ctx context.Context, msgs []Message) (string, error)
}
