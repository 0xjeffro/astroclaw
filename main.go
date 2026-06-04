// Minimal CLI for testing the deployed backend. This is NOT the final client.
// It validates the core flow: HTTP session CRUD, Function URL reply, and
// WebSocket real-time push.
package main

import (
	"astroclaw/pkg/app/chat"
	"astroclaw/pkg/app/settings"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// remoteBackend talks to the deployed APIs via HTTP.
// API Gateway handles session CRUD, Function URL handles reply.
type remoteBackend struct {
	apiURL   string // API Gateway URL for session CRUD
	replyURL string // Function URL for reply (no 30s timeout)
	apiKey   string
	client   *http.Client
}

func newRemoteBackend(apiURL, replyURL, apiKey string) *remoteBackend {
	return &remoteBackend{
		apiURL:   strings.TrimRight(apiURL, "/"),
		replyURL: strings.TrimRight(replyURL, "/"),
		apiKey:   apiKey,
		client:   &http.Client{},
	}
}

func (r *remoteBackend) CreateSessionInWorkspace(ctx context.Context, workspaceID, userID string, agentIDs []string, title string) (*chat.Session, error) {
	body, _ := json.Marshal(map[string]any{"user_id": userID, "agent_ids": agentIDs, "title": title})
	resp, err := r.post(ctx, "/workspaces/"+workspaceID+"/sessions", body)
	if err != nil {
		return nil, err
	}
	var s chat.Session
	if err := json.Unmarshal(resp, &s); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &s, nil
}

func (r *remoteBackend) ListSessionsInWorkspace(ctx context.Context, workspaceID string) ([]*chat.Session, error) {
	resp, err := r.get(ctx, "/workspaces/"+workspaceID+"/sessions")
	if err != nil {
		return nil, err
	}
	var sessions []*chat.Session
	if err := json.Unmarshal(resp, &sessions); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return sessions, nil
}

func (r *remoteBackend) GetSessionInWorkspace(ctx context.Context, workspaceID, id string) (*chat.Session, error) {
	resp, err := r.get(ctx, "/workspaces/"+workspaceID+"/sessions/"+id)
	if err != nil {
		return nil, err
	}
	var s chat.Session
	if err := json.Unmarshal(resp, &s); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &s, nil
}

func (r *remoteBackend) SoftDeleteSession(ctx context.Context, workspaceID, id string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", r.apiURL+"/workspaces/"+workspaceID+"/sessions/"+id, nil)
	if err != nil {
		return err
	}
	r.setHeaders(req)
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete failed: %s", body)
	}
	return nil
}

func (r *remoteBackend) GetAdminID(ctx context.Context) (string, error) {
	resp, err := r.get(ctx, "/users/admin")
	if err != nil {
		return "", err
	}
	var result struct {
		ID string `json:"ID"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("parse admin response: %w", err)
	}
	return result.ID, nil
}

func (r *remoteBackend) ListUserWorkspaces(ctx context.Context, userID string) ([]string, error) {
	resp, err := r.get(ctx, "/users/"+userID+"/workspaces")
	if err != nil {
		return nil, err
	}
	var result []struct {
		ID string `json:"ID"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("parse workspaces response: %w", err)
	}
	ids := make([]string, len(result))
	for i, w := range result {
		ids[i] = w.ID
	}
	return ids, nil
}

func (r *remoteBackend) GetWorkspaceSetting(ctx context.Context, workspaceID, name string) (string, error) {
	resp, err := r.get(ctx, "/workspaces/"+workspaceID+"/settings/"+name)
	if err != nil {
		return "", err
	}
	var result struct {
		Value string `json:"Value"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("parse setting response: %w", err)
	}
	return result.Value, nil
}

// ReplyToSession Reply TODO: implement streaming for remote mode. Currently waits for the full
// response before returning, so the user sees no output until it's complete.
func (r *remoteBackend) ReplyToSession(ctx context.Context, sessionID, agentID, text string) (string, error) {
	body, _ := json.Marshal(map[string]string{"text": text, "agent_id": agentID})
	resp, err := r.postTo(ctx, r.replyURL, "/sessions/"+sessionID+"/reply", body)
	if err != nil {
		return "", err
	}
	var result struct {
		Reply string `json:"reply"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	return result.Reply, nil
}

func (r *remoteBackend) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", r.apiURL+path, nil)
	if err != nil {
		return nil, err
	}
	r.setHeaders(req)
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}
	return body, nil
}

func (r *remoteBackend) post(ctx context.Context, path string, body []byte) ([]byte, error) {
	return r.postTo(ctx, r.apiURL, path, body)
}

func (r *remoteBackend) postTo(ctx context.Context, baseURL, path string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	r.setHeaders(req)
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, respBody)
	}
	return respBody, nil
}

func (r *remoteBackend) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if r.apiKey != "" {
		req.Header.Set("x-api-key", r.apiKey)
	}
}

func main() {
	ctx := context.Background()

	apiURL := os.Getenv("API_URL")
	if apiURL == "" {
		log.Fatal("API_URL must be set")
	}
	replyURL := os.Getenv("REPLY_URL")
	if replyURL == "" {
		log.Fatal("REPLY_URL must be set")
	}
	wsURL := os.Getenv("WS_URL")
	if wsURL == "" {
		log.Fatal("WS_URL must be set")
	}
	backend := newRemoteBackend(apiURL, replyURL, os.Getenv("API_KEY"))
	fmt.Printf("AstroClaw (remote: %s) - type /exit to quit\n", apiURL)

	// Fetch admin user ID, admin's workspace, and default agent ID.
	adminID, err := backend.GetAdminID(ctx)
	if err != nil {
		log.Fatalf("failed to get admin: %v", err)
	}
	workspaces, err := backend.ListUserWorkspaces(ctx, adminID)
	if err != nil {
		log.Fatalf("failed to list admin workspaces: %v", err)
	}
	if len(workspaces) == 0 {
		log.Fatalf("admin has no workspaces")
	}

	// TODO: support switch workspace
	workspaceID := workspaces[0]
	defaultAgentID, err := backend.GetWorkspaceSetting(ctx, workspaceID, settings.SettingWorkspaceDefaultAgentID)
	if err != nil {
		log.Fatalf("failed to get default agent: %v", err)
	}

	wsConn, _, err := websocket.DefaultDialer.Dial(
		wsURL+"?user_id="+adminID+"&workspace_id="+workspaceID+"&api_key="+os.Getenv("API_KEY"),
		nil,
	)
	if err != nil {
		log.Fatalf("connect to WebSocket: %v", err)
	}
	defer func() { _ = wsConn.Close() }()

	// Send WebSocket ping every 5 minutes to prevent API Gateway's 10-minute idle timeout.
	// https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-execution-service-websocket-limits-table.html
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			if err := wsConn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("ws ping error: %v", err)
				return
			}
		}
	}()

	wsDone := make(chan struct{})
	go func() {
		printedTools := map[string]bool{}
		var lastTextLen int
		for {
			_, msg, err := wsConn.ReadMessage()
			if err != nil {
				log.Printf("ws read error: %v", err)
				return
			}
			var event chat.WSEvent
			err = json.Unmarshal(msg, &event)
			if err != nil {
				return
			}

			// DEBUG: uncomment to trace WebSocket events
			log.Printf("[ws] status=%s text_len=%d tool_calls=%d", event.Status, len(event.Text), len(event.ToolCalls))

			switch event.Status {
			case chat.WSStatusStreaming:
				if len(event.Text) > lastTextLen {
					fmt.Print(event.Text[lastTextLen:])
					lastTextLen = len(event.Text)
				}
			case chat.WSStatusToolCalling:
				for _, tc := range event.ToolCalls {
					if tc.Status == chat.WSToolStatusRunning && !printedTools[tc.ID] {
						fmt.Printf("\n[calling %s(%s)]\n", tc.Name, tc.Arguments)
						printedTools[tc.ID] = true
					}
				}
			case chat.WSStatusDone:
				// Don't print event.Text here. The Reply Lambda's final done
				// event includes the full reply text, but we already printed
				// it incrementally via streaming events. Just reset state.
				fmt.Println()
				lastTextLen = 0
				printedTools = map[string]bool{}
				wsDone <- struct{}{}
			case chat.WSStatusError:
				_, err := fmt.Fprintf(os.Stderr, "\nerror: %s\n", event.Error)
				if err != nil {
					return
				}
				wsDone <- struct{}{}
			}
		}
	}()

	// Create a default session on startup with the admin and default agent.
	defaultSession, err := backend.CreateSessionInWorkspace(ctx, workspaceID, adminID, []string{defaultAgentID}, "default")
	if err != nil {
		log.Fatal(err)
	}
	currentSession := defaultSession.ID
	fmt.Printf("Session: %s (%s)\n", defaultSession.Title, defaultSession.ID)

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			return
		}
		input := scanner.Text()
		switch {
		case input == "/exit" || input == "/quit":
			fmt.Println()
			return
		case input == "/sessions":
			sessions, err := backend.ListSessionsInWorkspace(ctx, workspaceID)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
				continue
			}
			for _, s := range sessions {
				marker := "  "
				if s.ID == currentSession {
					marker = "* "
				}
				fmt.Printf("%s%s (%s)\n", marker, s.Title, s.ID)
			}
		case strings.HasPrefix(input, "/new"):
			title := strings.TrimSpace(strings.TrimPrefix(input, "/new"))
			if title == "" {
				title = "untitled"
			}
			s, err := backend.CreateSessionInWorkspace(ctx, workspaceID, adminID, []string{defaultAgentID}, title)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
				continue
			}
			currentSession = s.ID
			fmt.Printf("Created and switched to session: %s (%s)\n", s.Title, s.ID)
		case strings.HasPrefix(input, "/switch "):
			id := strings.TrimSpace(strings.TrimPrefix(input, "/switch "))
			if _, err := backend.GetSessionInWorkspace(ctx, workspaceID, id); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
				continue
			}
			currentSession = id
			fmt.Printf("Switched to session: %s\n", id)
		case strings.HasPrefix(input, "/delete "):
			id := strings.TrimSpace(strings.TrimPrefix(input, "/delete "))
			if id == currentSession {
				_, _ = fmt.Fprintln(os.Stderr, "error: cannot delete the active session")
				continue
			}
			if err := backend.SoftDeleteSession(ctx, workspaceID, id); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
				continue
			}
			fmt.Printf("Deleted session: %s\n", id)
		case input == "":
			continue
		default:
			go func() {
				if _, err := backend.ReplyToSession(ctx, currentSession, defaultAgentID, input); err != nil {
					log.Printf("reply error: %v", err)
					wsDone <- struct{}{}
				}
			}()
			<-wsDone
		}
	}
}
