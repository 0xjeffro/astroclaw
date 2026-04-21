package chat

// convert.go converts between store.Message and provider.Message.
// These two types carry the same conversation data but live in different
// packages: provider.Message has JSON tags for LLM API serialization,
// store.Message has persistence metadata (ID, SessionID, timestamps).
// This file bridges the two so SessionManager can load from Store into
// Agent and save from Agent back to Store.

import (
	"encoding/json"
	"iclaw/pkg/provider"
)

// ToProviderMessages converts store messages to provider messages
// for feeding into Agent. Agent doesn't know about store types.
func ToProviderMessages(msgs []Message) []provider.Message {
	out := make([]provider.Message, len(msgs))
	for i, m := range msgs {
		out[i] = provider.Message{
			Role:       m.Role,
			Content:    m.Content,
			ToolCalls:  m.ToolCalls,
			ToolCallID: m.ToolCallID,
		}
	}
	return out
}

// providerToChatMessages converts provider messages to chat messages
// for persisting Agent output back to the database. SessionID is set on
// each message; SequenceNumber is left at 0 for Store to auto-assign.
func providerToChatMessages(sessionID string, msgs []provider.Message) []Message {
	out := make([]Message, len(msgs))
	for i, m := range msgs {
		out[i] = Message{
			SessionID:  sessionID,
			Role:       m.Role,
			Content:    m.Content,
			ToolCalls:  m.ToolCalls,
			ToolCallID: m.ToolCallID,
		}
	}
	return out
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("[]")
	}
	return b
}
