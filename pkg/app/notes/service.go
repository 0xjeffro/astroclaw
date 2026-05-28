package notes

import (
	"context"
	"fmt"

	"astroclaw/pkg/app/notes/db"

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

// SaveMemoryForUser SaveMemory creates a new memory entry attributed to (agentID, userID) and
// links it to the source messages it was derived from.
func (svc *Service) SaveMemoryForUser(ctx context.Context, agentID, userID, content, sessionID string, messageIDs []string) (*Memory, error) {
	m, err := svc.queries.CreateMemoryForUser(ctx, db.CreateMemoryForUserParams{
		AgentID:   agentID,
		UserID:    userID,
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

// ListRecentMemoriesByAgentAndUser ListRecentMemories returns the most recent n memories that agent X has
// recorded about user Y.
func (svc *Service) ListRecentMemoriesByAgentAndUser(ctx context.Context, agentID, userID string, limit int32) ([]*Memory, error) {
	rows, err := svc.queries.ListRecentMemoriesByAgentAndUser(ctx, db.ListRecentMemoriesByAgentAndUserParams{
		AgentID: agentID,
		UserID:  userID,
		Limit:   limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list recent memories: %w", err)
	}

	memories := make([]*Memory, len(rows))
	for i, m := range rows {
		memories[i] = MemoryFromDB(m, nil)
	}
	return memories, nil
}

// FormatUserMemoryForPrompt FormatForPrompt returns the memories that agent X has recorded about user Y
// as a single string for injection into the system prompt. Each memory is one line.
func (svc *Service) FormatUserMemoryForPrompt(ctx context.Context, agentID, userID string, maxChars int) (string, error) {
	rows, err := svc.queries.ListMemoriesByAgentAndUser(ctx, db.ListMemoriesByAgentAndUserParams{
		AgentID: agentID,
		UserID:  userID,
	})
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
