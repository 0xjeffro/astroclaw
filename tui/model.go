package main

import (
	"context"
	"fmt"
	"strings"

	"astroclaw/pkg/app/chat"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gorilla/websocket"
)

// chatMessage represents a single message displayed in the chat viewport.
type chatMessage struct {
	role    string // "user" or "agent"
	content string
}

// model is the Bubble Tea model for the TUI.
type model struct {
	backend        *backend
	wsConn         *websocket.Conn
	sessionID      string
	ownerID        string
	defaultAgentID string

	viewport viewport.Model
	input    textinput.Model
	messages []chatMessage

	// Streaming state: accumulates the agent's response as events arrive.
	streaming      bool
	streamingText  string
	streamingTools []chat.WSToolCall

	width  int
	height int
	ready  bool
}

// replyDoneMsg is sent when the HTTP reply goroutine finishes (success or error).
type replyDoneMsg struct {
	err error
}

func newModel(b *backend, ws *websocket.Conn, sessionID, ownerID, defaultAgentID string) model {
	ti := textinput.New()
	ti.Placeholder = "Type a message..."
	ti.Focus()

	return model{
		backend:        b,
		wsConn:         ws,
		sessionID:      sessionID,
		ownerID:        ownerID,
		defaultAgentID: defaultAgentID,
		input:          ti,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		listenWS(m.wsConn),
		pingWS(m.wsConn),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.MouseMsg:
		// Scroll 3 lines per wheel tick for smoother scrolling.
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.viewport.LineUp(3)
		case tea.MouseButtonWheelDown:
			m.viewport.LineDown(3)
		}

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit

		case tea.KeyEnter:
			text := strings.TrimSpace(m.input.Value())
			if text == "" {
				return m, nil
			}

			// Add user message to chat.
			m.messages = append(m.messages, chatMessage{role: "user", content: text})
			m.input.Reset()
			m.streaming = true
			m.streamingText = ""
			m.streamingTools = nil
			updateViewport(&m)

			// Send reply via HTTP in background.
			sessionID := m.sessionID
			agentID := m.defaultAgentID
			cmds = append(cmds, func() tea.Msg {
				err := m.backend.reply(context.Background(), sessionID, agentID, text)
				return replyDoneMsg{err: err}
			})
			return m, tea.Batch(cmds...)
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		inputHeight := 3 // input box height
		vpWidth := msg.Width
		vpHeight := msg.Height - inputHeight

		if !m.ready {
			m.viewport = viewport.New(vpWidth, vpHeight)
			m.ready = true
		} else {
			m.viewport.Width = vpWidth
			m.viewport.Height = vpHeight
		}
		m.input.Width = vpWidth - 4
		updateViewport(&m)

	case wsEventMsg:
		event := chat.WSEvent(msg)
		switch event.Status {
		case chat.WSStatusStreaming:
			m.streaming = true
			m.streamingText = event.Text
			m.streamingTools = event.ToolCalls
		case chat.WSStatusToolCalling:
			m.streaming = true
			m.streamingTools = event.ToolCalls
		case chat.WSStatusDone:
			if m.streamingText != "" {
				m.messages = append(m.messages, chatMessage{role: "agent", content: m.streamingText})
			}
			m.streaming = false
			m.streamingText = ""
			m.streamingTools = nil
		case chat.WSStatusError:
			m.messages = append(m.messages, chatMessage{role: "agent", content: "Error: " + event.Error})
			m.streaming = false
			m.streamingText = ""
			m.streamingTools = nil
		}
		updateViewport(&m)
		// Keep listening for more WebSocket events.
		cmds = append(cmds, listenWS(m.wsConn))

	case wsPingMsg:
		// Ping sent, schedule next one.
		cmds = append(cmds, pingWS(m.wsConn))

	case replyDoneMsg:
		if msg.err != nil && !m.streaming {
			m.messages = append(m.messages, chatMessage{role: "agent", content: "Error: " + msg.err.Error()})
			updateViewport(&m)
		}
	}

	// Update sub-components.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	cmds = append(cmds, cmd)
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// updateViewport rebuilds the chat content and scrolls to the bottom.
func updateViewport(m *model) {
	var b strings.Builder

	for _, msg := range m.messages {
		b.WriteString(formatMessage(msg))
		b.WriteByte('\n')
	}

	// Show streaming content (agent is currently responding).
	if m.streaming {
		// Show tool calls in progress.
		for _, tc := range m.streamingTools {
			icon := "⟳"
			if tc.Status == chat.WSToolStatusCompleted {
				icon = "✓"
			} else if tc.Status == chat.WSToolStatusFailed {
				icon = "✗"
			}
			b.WriteString(fmt.Sprintf("  [%s %s]\n", icon, tc.Name))
		}
		if m.streamingText != "" {
			b.WriteString(formatMessage(chatMessage{role: "agent", content: m.streamingText}))
			b.WriteByte('\n')
		}
	}

	// Wrap text to viewport width so long lines don't overflow horizontally.
	wrapped := lipgloss.NewStyle().Width(m.viewport.Width).Render(b.String())
	m.viewport.SetContent(wrapped)
	m.viewport.GotoBottom()
}

func (m model) View() string {
	if !m.ready {
		return "Loading..."
	}

	inputBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#F45562")).
		Padding(0, 1).
		Width(m.width - 2).
		Render(m.input.View())

	return m.viewport.View() + "\n" + inputBox
}

func formatMessage(msg chatMessage) string {
	var style lipgloss.Style
	var prefix string

	if msg.role == "user" {
		prefix = "You"
		style = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	} else {
		prefix = "Agent"
		style = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	}

	return style.Render(prefix) + "\n" + msg.content
}
