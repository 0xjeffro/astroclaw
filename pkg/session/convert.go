package session

// convert.go converts between store.Message and provider.Message.
// These two types carry the same conversation data but live in different
// packages: provider.Message has JSON tags for LLM API serialization,
// store.Message has persistence metadata (ID, SessionID, timestamps).
// This file bridges the two so SessionManager can load from Store into
// Agent and save from Agent back to Store.

import (
	"iclaw/pkg/provider"
	"iclaw/pkg/store"
)

// StoreToProviderMessages converts store messages to provider messages
// for feeding into Agent. Agent doesn't know about store types.
func StoreToProviderMessages(msgs []store.Message) []provider.Message {
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

// providerToStoreMessages converts provider messages to store messages
// for persisting Agent output back to the database. SessionID is set on
// each message; SequenceNumber is left at 0 for Store to auto-assign.
func providerToStoreMessages(sessionID string, msgs []provider.Message) []store.Message {
	out := make([]store.Message, len(msgs))
	for i, m := range msgs {
		out[i] = store.Message{
			SessionID:  sessionID,
			Role:       m.Role,
			Content:    m.Content,
			ToolCalls:  m.ToolCalls,
			ToolCallID: m.ToolCallID,
		}
	}
	return out
}
