package notes

import (
	"time"

	"iclaw/pkg/app/notes/db"
)

type Memory struct {
	ID         string
	Content    string
	SessionID  string
	MessageIDs []string // source messages this memory was derived from
	CreatedAt  time.Time
}

func MemoryFromDB(m db.AppNotesMemory, messageIDs []string) *Memory {
	return &Memory{
		ID:         m.ID,
		Content:    m.Content,
		SessionID:  m.SessionID,
		MessageIDs: messageIDs,
		CreatedAt:  m.CreatedAt,
	}
}
