package notes

import "context"

// MemoryStoreAdapter wraps notes.Service to satisfy tool.MemoryStore interface.
type MemoryStoreAdapter struct {
	Service *Service
}

func (a *MemoryStoreAdapter) SaveMemory(ctx context.Context, content, sessionID string, messageIDs []string) error {
	_, err := a.Service.SaveMemory(ctx, content, sessionID, messageIDs)
	return err
}
