package notes

import "context"

// MemoryStoreAdapter wraps notes.Service to satisfy tool.MemoryStore interface.
type MemoryStoreAdapter struct {
	Service *Service
}

func (a *MemoryStoreAdapter) SaveMemory(ctx context.Context, agentID, content, sessionID string, messageIDs []string) error {
	_, err := a.Service.SaveMemory(ctx, agentID, content, sessionID, messageIDs)
	return err
}
