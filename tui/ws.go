package main

import (
	"encoding/json"
	"log"

	"astroclaw/pkg/app/chat"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gorilla/websocket"
)

// wsEventMsg wraps a WSEvent as a Bubble Tea message so it can be
// delivered to the Model's Update function via the event loop.
type wsEventMsg chat.WSEvent

// connectWS establishes a WebSocket connection to the API Gateway.
func connectWS(wsURL, userID, apiKey string) (*websocket.Conn, error) {
	conn, _, err := websocket.DefaultDialer.Dial(
		wsURL+"?user_id="+userID+"&api_key="+apiKey,
		nil,
	)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// listenWS returns a Bubble Tea command that reads WebSocket messages
// in a goroutine and sends them to the TUI as wsEventMsg.
// Bubble Tea commands are functions that return a tea.Msg. For streaming,
// we use tea.Sub-like pattern: return the first event, then re-subscribe.
func listenWS(conn *websocket.Conn) tea.Cmd {
	return func() tea.Msg {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Printf("ws read error: %v", err)
			return nil
		}
		var event chat.WSEvent
		if err := json.Unmarshal(msg, &event); err != nil {
			log.Printf("ws unmarshal error: %v", err)
			return nil
		}
		return wsEventMsg(event)
	}
}
