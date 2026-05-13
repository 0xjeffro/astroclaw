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

	// Fetch owner and default agent.
	ownerID, err := backend.getOwnerID(ctx)
	if err != nil {
		log.Fatalf("failed to get owner: %v", err)
	}
	defaultAgentID, err := backend.getSetting(ctx, "default_agent_id")
	if err != nil {
		log.Fatalf("failed to get default agent: %v", err)
	}

	// Create a default session.
	session, err := backend.newSession(ctx, ownerID, []string{defaultAgentID}, "default")
	if err != nil {
		log.Fatalf("failed to create session: %v", err)
	}

	// Connect WebSocket.
	ws, err := connectWS(wsURL, ownerID, apiKey)
	if err != nil {
		log.Fatalf("failed to connect WebSocket: %v", err)
	}

	model := newModel(backend, ws, session.ID, ownerID, defaultAgentID)

	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
