package notes

import (
	"context"
	"fmt"

	"iclaw/pkg/app/notes/db"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	queries *db.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{
		queries: db.New(pool),
	}
}

// SaveMemory creates a new memory entry and links it to the source messages
// it was derived from.
func (svc *Service) SaveMemory(ctx context.Context, content, sessionID string, messageIDs []string) (*Memory, error) {
	m, err := svc.queries.CreateMemory(ctx, db.CreateMemoryParams{
		Content:   content,
		SessionID: sessionID,
	})
	if err != nil {
		return nil, fmt.Errorf("create memory: %w", err)
	}

	for _, msgID := range messageIDs {
		if err := svc.queries.AddMemorySource(ctx, db.AddMemorySourceParams{
			MemoryID:  m.ID,
			MessageID: msgID,
		}); err != nil {
			return nil, fmt.Errorf("add memory source: %w", err)
		}
	}

	return MemoryFromDB(m, messageIDs), nil
}

// ListRecentMemories returns the most recent n memories.
func (svc *Service) ListRecentMemories(ctx context.Context, limit int32) ([]*Memory, error) {
	rows, err := svc.queries.ListRecentMemories(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent memories: %w", err)
	}

	memories := make([]*Memory, len(rows))
	for i, m := range rows {
		memories[i] = MemoryFromDB(m, nil)
	}
	return memories, nil
}

// FormatForPrompt returns all memories as a single string for injection
// into the system prompt. Each memory is one line.
func (svc *Service) FormatForPrompt(ctx context.Context, maxChars int) (string, error) {
	rows, err := svc.queries.ListMemories(ctx)
	if err != nil {
		return "", fmt.Errorf("list memories: %w", err)
	}

	var result string
	for _, m := range rows {
		line := "- " + m.Content + "\n"
		if len(result)+len(line) > maxChars {
			break
		}
		result += line
	}
	return result, nil
}
