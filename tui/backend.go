package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// backend talks to the deployed APIs via HTTP.
type backend struct {
	apiURL   string
	replyURL string
	apiKey   string
	client   *http.Client
}

func newBackend(apiURL, replyURL, apiKey string) *backend {
	return &backend{
		apiURL:   strings.TrimRight(apiURL, "/"),
		replyURL: strings.TrimRight(replyURL, "/"),
		apiKey:   apiKey,
		client:   &http.Client{},
	}
}

type sessionInfo struct {
	ID    string `json:"ID"`
	Title string `json:"Title"`
}

func (b *backend) getAdminID(ctx context.Context) (string, error) {
	resp, err := b.get(ctx, "/users/admin")
	if err != nil {
		return "", err
	}
	var result struct {
		ID string `json:"ID"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("parse admin: %w", err)
	}
	return result.ID, nil
}

func (b *backend) listUserWorkspaces(ctx context.Context, userID string) ([]string, error) {
	resp, err := b.get(ctx, "/users/"+userID+"/workspaces")
	if err != nil {
		return nil, err
	}
	var result []struct {
		ID string `json:"ID"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("parse workspaces: %w", err)
	}
	ids := make([]string, len(result))
	for i, w := range result {
		ids[i] = w.ID
	}
	return ids, nil
}

func (b *backend) getSetting(ctx context.Context, name string) (string, error) {
	resp, err := b.get(ctx, "/settings/"+name)
	if err != nil {
		return "", err
	}
	var result struct {
		Value string `json:"Value"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("parse setting: %w", err)
	}
	return result.Value, nil
}

func (b *backend) newSession(ctx context.Context, userID string, agentIDs []string, title string) (*sessionInfo, error) {
	body, _ := json.Marshal(map[string]any{"user_id": userID, "agent_ids": agentIDs, "title": title})
	resp, err := b.post(ctx, "/sessions", body)
	if err != nil {
		return nil, err
	}
	var s sessionInfo
	if err := json.Unmarshal(resp, &s); err != nil {
		return nil, fmt.Errorf("parse session: %w", err)
	}
	return &s, nil
}

func (b *backend) listSessions(ctx context.Context) ([]sessionInfo, error) {
	resp, err := b.get(ctx, "/sessions")
	if err != nil {
		return nil, err
	}
	var sessions []sessionInfo
	if err := json.Unmarshal(resp, &sessions); err != nil {
		return nil, fmt.Errorf("parse sessions: %w", err)
	}
	return sessions, nil
}

func (b *backend) reply(ctx context.Context, sessionID, agentID, text string) error {
	body, _ := json.Marshal(map[string]string{"text": text, "agent_id": agentID})
	_, err := b.postTo(ctx, b.replyURL, "/sessions/"+sessionID+"/reply", body)
	return err
}

func (b *backend) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", b.apiURL+path, nil)
	if err != nil {
		return nil, err
	}
	b.setHeaders(req)
	return b.doRequest(req)
}

func (b *backend) post(ctx context.Context, path string, body []byte) ([]byte, error) {
	return b.postTo(ctx, b.apiURL, path, body)
}

func (b *backend) postTo(ctx context.Context, baseURL, path string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	b.setHeaders(req)
	return b.doRequest(req)
}

func (b *backend) doRequest(req *http.Request) ([]byte, error) {
	resp, err := b.client.Do(req)
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

func (b *backend) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if b.apiKey != "" {
		req.Header.Set("x-api-key", b.apiKey)
	}
}
