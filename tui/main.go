package main

import (
	"context"
	"fmt"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
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
	apiKey := os.Getenv("API_KEY")

	ctx := context.Background()
	backend := newBackend(apiURL, replyURL, apiKey)

	// Fetch admin, workspace, and default agent.
	adminID, err := backend.getAdminID(ctx)
	if err != nil {
		log.Fatalf("failed to get admin: %v", err)
	}
	workspaces, err := backend.listUserWorkspaces(ctx, adminID)
	if err != nil {
		log.Fatalf("failed to list workspaces: %v", err)
	}
	if len(workspaces) == 0 {
		log.Fatalf("admin has no workspaces")
	}
	workspaceID := workspaces[0]
	defaultAgentID, err := backend.getSetting(ctx, "default_agent_id")
	if err != nil {
		log.Fatalf("failed to get default agent: %v", err)
	}

	// Create a default session.
	session, err := backend.createSessionInWorkspace(ctx, workspaceID, adminID, []string{defaultAgentID}, "default")
	if err != nil {
		log.Fatalf("failed to create session: %v", err)
	}

	// Connect WebSocket.
	ws, err := connectWS(wsURL, adminID, workspaceID, apiKey)
	if err != nil {
		log.Fatalf("failed to connect WebSocket: %v", err)
	}

	model := newModel(backend, ws, workspaceID, session.ID, adminID, defaultAgentID)

	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
