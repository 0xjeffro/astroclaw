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

// Layout constants for the TUI.
const (
	// sidebarTotalWidth is the session sidebar width including border (20 content + 2 border).
	sidebarTotalWidth = 30
	// rightSidebarMinWidth is the minimum width to show the log sidebar.
	rightSidebarMinWidth = 25
	// rightSidebarMaxWidth is the maximum width the log sidebar can grow to.
	rightSidebarMaxWidth = 80
	// chatMinWidth is the minimum width for the chat area. Right sidebar
	// collapses before chat gets narrower than this.
	chatMinWidth = 61
	// inputBoxHeight is the height of the bottom input area including border.
	inputBoxHeight = 3
)

// calcRightSidebarWidth returns how wide the right sidebar should be.
// Extra space goes to logs first (up to max), then to chat.
// Returns 0 if there's not enough room.
func calcRightSidebarWidth(terminalWidth int) int {
	available := terminalWidth - sidebarTotalWidth - chatMinWidth - 4 // 4 for chat border+padding
	if available < rightSidebarMinWidth {
		return 0 // not enough room, hide it
	}
	if available > rightSidebarMaxWidth {
		return rightSidebarMaxWidth
	}
	return available
}

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

	// ------ Session Column State ------
	sessions    []sessionInfo   // all sessions
	selectedIdx int             // index of the currently selected session
	collapsed   map[string]bool // which sidebar sections are collapsed

	// ------ Chat Column State ------
	viewport viewport.Model
	input    textinput.Model
	messages []chatMessage

	// Streaming state: accumulates the agent's response as events arrive.
	streaming      bool
	streamingText  string
	streamingTools []chat.WSToolCall

	// ------ Log Sidebar State ------
	logs []string // log entries displayed in the right sidebar

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
		sessions: []sessionInfo{
			{ID: sessionID, Title: "Default"},
		},
		selectedIdx: 0,
		collapsed: map[string]bool{
			"agents": false,
			"groups": false,
			"chats":  false,
		},
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
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.viewport.ScrollUp(3)
		case tea.MouseButtonWheelDown:
			m.viewport.ScrollDown(3)
		case tea.MouseButtonLeft:
			// Toggle sidebar section collapse on click (press only, not release).
			if msg.Action != tea.MouseActionPress {
				break
			}
			if msg.X < sidebarTotalWidth {
				for section, y := range sectionHeaderYPositions {
					if msg.Y == y {
						m.collapsed[section] = !m.collapsed[section]
						break
					}
				}
			}
		default:
			// ignore other mouse events
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

		rightWidth := calcRightSidebarWidth(msg.Width)
		vpWidth := msg.Width - sidebarTotalWidth - rightWidth - 4 // subtract viewport border(2) + padding(2)
		vpHeight := msg.Height - inputBoxHeight - 2

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

	// Session title at the top.
	if m.selectedIdx < len(m.sessions) {
		title := m.sessions[m.selectedIdx].Title
		titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#9B9DA0"))
		b.WriteString(titleStyle.Render("# "+title) + "\n\n")
	}

	for _, msg := range m.messages {
		b.WriteString(formatMessage(msg))
		b.WriteString("\n\n")
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

	rightWidth := calcRightSidebarWidth(m.width)
	chatWidth := m.width - sidebarTotalWidth - rightWidth - 2

	inputBox := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#F45562")).
		Padding(0, 1).
		Width(chatWidth).
		Render(m.input.View())

	viewportBox := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#9B9DA0")).
		Padding(0, 1).
		Width(chatWidth).
		Render(m.viewport.View())

	chatPanel := viewportBox + "\n" + inputBox
	chatHeight := lipgloss.Height(chatPanel)

	sidebar := renderSessions(m.sessions, m.selectedIdx, m.collapsed, chatHeight)

	if rightWidth == 0 {
		return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, chatPanel)
	}

	logPanel := renderLogs(m.logs, chatHeight, rightWidth)
	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, chatPanel, logPanel)
}

// sectionHeaderYPositions tracks Y coordinates of section headers for click detection.
var sectionHeaderYPositions = map[string]int{}

func renderSessions(sessions []sessionInfo, selectedIdx int, collapsed map[string]bool, height int) string {
	contentWidth := sidebarTotalWidth - 2 // subtract border width
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B6B6B"))
	accentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F45562"))

	// sectionHeader renders: "  ⌄ Label (3) ──── [+]" or "  › Label (3) ──── [+]"
	sectionHeader := func(label string, count int, isCollapsed bool) string {
		icon := "⌄"
		if isCollapsed {
			icon = "›"
		}
		text := accentStyle.Render("  "+icon+" "+label) + " " + dimStyle.Render(fmt.Sprintf("(%d)", count)) + " "
		plus := accentStyle.Render("[+]")
		textWidth := lipgloss.Width(text)
		plusWidth := lipgloss.Width(plus)
		lineRight := contentWidth - textWidth - plusWidth - 2
		if lineRight < 0 {
			lineRight = 0
		}
		return text + dimStyle.Render(strings.Repeat("─", lineRight)) + " " + plus
	}

	var list strings.Builder
	lineNum := 1 // start after top border

	// Section: Agents
	list.WriteString("\n")
	lineNum++
	sectionHeaderYPositions["agents"] = lineNum
	list.WriteString(sectionHeader("👨🏻‍🚀 Agents", 0, collapsed["agents"]) + "\n")
	lineNum++
	if !collapsed["agents"] {
		list.WriteString(dimStyle.Render("    (none)") + "\n")
		lineNum++
	}

	// Section: Groups
	list.WriteString("\n")
	lineNum++
	sectionHeaderYPositions["groups"] = lineNum
	list.WriteString(sectionHeader("Groups", 0, collapsed["groups"]) + "\n")
	lineNum++
	if !collapsed["groups"] {
		list.WriteString(dimStyle.Render("    (none)") + "\n")
		lineNum++
	}

	// Section: Chats
	list.WriteString("\n")
	lineNum++
	sectionHeaderYPositions["chats"] = lineNum
	list.WriteString(sectionHeader("Chats", len(sessions), collapsed["chats"]) + "\n")
	lineNum++
	if !collapsed["chats"] {
		for i, s := range sessions {
			if i == selectedIdx {
				list.WriteString("    * " + s.Title + "\n")
			} else {
				list.WriteString("      " + s.Title + "\n")
			}
			lineNum++
		}
	}

	contentHeight := height - 2 // subtract top label + bottom border

	// Pad the middle so [+ New] is pushed to the bottom.
	listLines := strings.Count(list.String(), "\n")
	padding := contentHeight - listLines - 1 // -1 for the [+ New] line
	if padding < 0 {
		padding = 0
	}

	var b strings.Builder
	b.WriteString(list.String())
	b.WriteString(strings.Repeat("\n", padding))
	settingsBtn := lipgloss.NewStyle().Foreground(lipgloss.Color("#F45562")).Render("  [⚙ Settings]")
	b.WriteString(settingsBtn)
	borderColor := lipgloss.Color("#9B9DA0")

	// Build top border with label: "─ 💬 Sessions ─────"
	label := " 🗂️ "
	labelBold := lipgloss.NewStyle().Bold(true).Render("Sessions") + " "
	labelDisplay := label + labelBold
	// len() counts bytes, but we need visual width for alignment.
	labelWidth := lipgloss.Width(labelDisplay)
	remaining := contentWidth - labelWidth
	left := 1
	right := remaining - left
	if right < 0 {
		right = 0
	}
	border := lipgloss.NewStyle().Foreground(borderColor)
	topBorder := border.Render("┌"+strings.Repeat("─", left)) +
		labelDisplay +
		border.Render(strings.Repeat("─", right)+"┐")

	// Render content with left, right, bottom borders (top is custom above).
	content := lipgloss.NewStyle().
		Width(contentWidth).
		Height(height - 2). // total height minus top label line(1) and bottom border(1)
		Border(lipgloss.NormalBorder()).
		BorderTop(false).
		BorderForeground(borderColor).
		Render(b.String())

	return topBorder + "\n" + content
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

func renderLogs(logs []string, height int, totalWidth int) string {
	contentWidth := totalWidth - 2
	borderColor := lipgloss.Color("#9B9DA0")
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B6B6B"))
	accentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F45562"))

	// Top border with label.
	label := " 📋 "
	labelBold := lipgloss.NewStyle().Bold(true).Render("Logs") + " "
	labelDisplay := label + labelBold
	labelWidth := lipgloss.Width(labelDisplay)
	remaining := contentWidth - labelWidth
	left := 1
	right := remaining - left
	if right < 0 {
		right = 0
	}
	border := lipgloss.NewStyle().Foreground(borderColor)
	topBorder := border.Render("┌"+strings.Repeat("─", left)) +
		labelDisplay +
		border.Render(strings.Repeat("─", right)+"┐")

	// Toolbar: [Search] [Clear]
	toolbar := "  " + dimStyle.Render("[Search]") + " " + accentStyle.Render("[Clear]")

	// Log entries.
	var b strings.Builder
	b.WriteString(toolbar + "\n\n")
	if len(logs) == 0 {
		b.WriteString(dimStyle.Render("  No logs yet") + "\n")
	} else {
		for _, entry := range logs {
			b.WriteString("  " + dimStyle.Render(entry) + "\n")
		}
	}

	content := lipgloss.NewStyle().
		Width(contentWidth).
		Height(height - 2).
		Border(lipgloss.NormalBorder()).
		BorderTop(false).
		BorderForeground(borderColor).
		Render(b.String())

	return topBorder + "\n" + content
}
