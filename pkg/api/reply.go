package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"astroclaw/pkg/app/chat"
	"astroclaw/pkg/cloud/wsbus"

	"github.com/go-chi/chi/v5"
)

// replyChatService captures the chat.Service subset used by ReplyHandler.
// Split from the api handlers' chatService because Reply is heavyweight
// (runs the agent) and Not always wired.
type replyChatService interface {
	Reply(ctx context.Context, sessionID, agentID, text string) (string, error)
	ListSessionMembers(ctx context.Context, sessionID string) ([]*chat.SessionMember, error)
}

// ReplyHandler serves POST /sessions/{sessionID}/reply. It runs the
// agent via Chat.Reply, pushes streaming updates and the final reply
// via wsbus.Bus, and returns the final reply as JSON.
//
// Chat is settable after construction so a Lambda / server main can
// build the ReplyHandler first (needed by createFn callbacks for
// streaming), then create chat.Service, then plug it in.
type ReplyHandler struct {
	Chat     replyChatService
	Bus      wsbus.Bus
	Registry wsbus.Registry
}

// ServeHTTP handles POST /sessions/{sessionID}/reply.
func (h *ReplyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "sessionID missing"})
		return
	}

	var body struct {
		Text    string `json:"text"`
		AgentID string `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}

	ctx := r.Context()
	reply, err := h.Chat.Reply(ctx, sessionID, body.AgentID, body.Text)
	if err != nil {
		h.PushToSession(ctx, sessionID, chat.WSEvent{
			SessionID: sessionID,
			AgentID:   body.AgentID,
			Status:    chat.WSStatusError,
			Error:     err.Error(),
		})
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.PushToSession(ctx, sessionID, chat.WSEvent{
		SessionID: sessionID,
		AgentID:   body.AgentID,
		Status:    chat.WSStatusDone,
		Text:      reply,
	})
	writeJSON(w, http.StatusOK, map[string]string{"reply": reply})
}

// PushToSession dispatches a WSEvent to all live connections of every
// member of the session. Streaming callbacks (OnTextDelta / OnToolCall
// / OnToolResult on the agent) call this while the reply is being
// built, then ServeHTTP calls it once more with the final Done event.
//
// Delivery is best-effort: registry / bus errors are logged, not
// bubbled. wsbus.ErrConnGone triggers an Unregister so stale rows do
// not accumulate.
func (h *ReplyHandler) PushToSession(ctx context.Context, sessionID string, event chat.WSEvent) {
	members, err := h.Chat.ListSessionMembers(ctx, sessionID)
	if err != nil {
		log.Printf("pushToSession: list members for %s: %v", sessionID, err)
		return
	}
	payload, err := json.Marshal(event)
	if err != nil {
		log.Printf("pushToSession: marshal event: %v", err)
		return
	}
	for _, m := range members {
		conns, err := h.Registry.GetByUser(ctx, m.UserID)
		if err != nil {
			log.Printf("pushToSession: registry GetByUser %s: %v", m.UserID, err)
			continue
		}
		for _, c := range conns {
			err := h.Bus.Send(ctx, c, payload)
			if errors.Is(err, wsbus.ErrConnGone) {
				log.Printf("pushToSession: conn %s gone, unregistering", c)
				_ = h.Registry.Unregister(ctx, c)
				continue
			}
			if err != nil {
				log.Printf("pushToSession: send to %s: %v", c, err)
			}
		}
	}
}
