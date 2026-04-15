package main

import (
	"bufio"
	"context"
	"fmt"
	"iclaw/pkg/agent"
	"iclaw/pkg/provider"
	"iclaw/pkg/tool"
	"log"
	"os"
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

	a := agent.New(p, "Respond in plain text. Do not use LaTeX or markdown formatting.", registry, 128000)
	a.OnToolCall = func(id string, name string, args string) {
		_, _ = fmt.Fprintf(os.Stderr, "⚡ [%s] calling %s(%s)\n", id, name, args)
	}
	a.OnToolResult = func(id string, name string, result string) {
		_, _ = fmt.Fprintf(os.Stderr, "✓ [%s] %s → %s\n", id, name, result)
	}

	fmt.Println("iClaw - type /exit to quit")
	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			// EOF or Ctrl+D, quit
			return
		}
		input := scanner.Text()
		if input == "/exit" || input == "/quit" {
			fmt.Println()
			return
		}
		if input == "/clear" {
			a.ClearHistory()
			fmt.Println("History cleared")
			continue
		}
		if input == "/history" {
			fmt.Println("History:")
			for i, msg := range a.History() {
				fmt.Printf("%d. [%s] %s\n", i+1, msg.Role, msg.Content)
			}
			continue
		}
		if input == "" {
			continue
		}
		reply, err := a.Reply(context.Background(), input)
		if err != nil {
			log.Printf("error: %v", err)
			continue
		}
		log.Println(reply)
	}
}
