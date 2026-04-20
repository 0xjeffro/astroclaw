package main

import (
	"bufio"
	"context"
	"fmt"
	"iclaw/pkg/agent"
	"iclaw/pkg/provider"
	"iclaw/pkg/session"
	"iclaw/pkg/store"
	"iclaw/pkg/tool"
	"log"
	"os"
	"strings"
)

func main() {
	var p provider.Provider
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		p = provider.NewAnthropic(key, "claude-sonnet-4-20250514")
	} else if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		p = provider.NewOpenAI(key, "gpt-4o-mini")
	} else {
		log.Fatal("OPENAI_API_KEY or ANTHROPIC_API_KEY must be set")
	}

	registry := tool.NewRegistry()
	registry.Register(tool.GetCurrentTime)
	registry.Register(tool.EvalArithmetic)
	registry.Register(tool.ReadFile)
	registry.Register(tool.ExecCommand)
	registry.Register(tool.WriteFile)
	registry.Register(tool.EditFile)

	systemPrompt := "You are iClaw, an intelligent assistant that can perform various tasks by calling tools. " +
		"Use tools to get information, manipulate files, execute commands, and more. " +
		"Always think step by step and use tools whenever appropriate. " +
		"Only respond with plain text, do not use LaTeX or markdown formatting."

	// createAgent builds a fresh Agent from a session's saved context.
	createFn := func(s *store.Session) *agent.Agent {
		return agent.NewFromContext(
			p, systemPrompt, registry, 128000,
			session.StoreToProviderMessages(s.ContextMessages), s.ContextSummary,
		)
	}

	scanner := bufio.NewScanner(os.Stdin)

	// configAgent sets callbacks on each new Agent instance.
	configFn := func(a *agent.Agent) {
		a.OnToolCall = func(id string, name string, args string) {
			_, _ = fmt.Fprintf(os.Stderr, "⚡ [%s] calling %s(%s)\n", id, name, args)
		}
		a.OnToolResult = func(id string, name string, result string) {
			_, _ = fmt.Fprintf(os.Stderr, "✓ [%s] %s → %s\n", id, name, result)
		}
		a.OnApproval = func(toolName string, args string) bool {
			_, _ = fmt.Fprintf(os.Stderr, "⚠️  %s(%s)\nApprove? [y/N] ", toolName, args)
			scanner.Scan()
			return strings.ToLower(strings.TrimSpace(scanner.Text())) == "y"
		}
	}

	var st store.Store
	if connStr := os.Getenv("DATABASE_URL"); connStr != "" {
		pgStore, err := store.NewPostgresStore(connStr)
		if err != nil {
			log.Fatalf("connect to database: %v", err)
		}
		defer func() { _ = pgStore.Close() }()
		st = pgStore
	} else {
		st = store.NewMemoryStore()
	}

	mgr := session.NewManager(st, createFn, configFn)
	ctx := context.Background()

	// Create a default session on startup.
	defaultSession, err := mgr.NewSession(ctx, "default")
	if err != nil {
		log.Fatal(err)
	}
	currentSession := defaultSession.ID

	fmt.Println("iClaw - type /exit to quit")
	fmt.Printf("Session: %s (%s)\n", defaultSession.Title, defaultSession.ID)

	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			// EOF or Ctrl+D, quit
			return
		}
		input := scanner.Text()
		switch {
		case input == "/exit" || input == "/quit":
			fmt.Println()
			return
		case input == "/sessions":
			sessions, _ := mgr.ListSessions(ctx)
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
			s, err := mgr.NewSession(ctx, title)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
				continue
			}
			currentSession = s.ID
			fmt.Printf("Created and switched to session: %s (%s)\n", s.Title, s.ID)
		case strings.HasPrefix(input, "/switch "):
			id := strings.TrimSpace(strings.TrimPrefix(input, "/switch "))
			// Verify session exists.
			if _, err := mgr.GetSession(ctx, id); err != nil {
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
			if err := mgr.DeleteSession(ctx, id); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
				continue
			}
			fmt.Printf("Deleted session: %s\n", id)
		case input == "":
			continue
		default:
			reply, err := mgr.Reply(ctx, currentSession, input)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
				continue
			}
			log.Println(reply)
		}
	}
}
