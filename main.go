package main

import (
	"bufio"
	"context"
	"fmt"
	"iclaw/pkg/agent"
	"iclaw/pkg/provider"
	"log"
	"os"
)

func main() {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		log.Fatal("OPENAI_API_KEY not set")
	}

	p := provider.NewOpenAI(key, "gpt-4o-mini")
	a := agent.New(p, "")

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
