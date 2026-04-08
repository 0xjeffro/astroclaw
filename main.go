package main

import (
	"context"
	"iclaw/pkg/agent"
	"iclaw/pkg/provider"
	"log"
	"os"
	"strings"
)

func main() {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		log.Fatal("OPENAI_API_KEY not set")
	}

	if len(os.Args) < 2 {
		log.Fatal("Usage: go run main.go <user input>")
	}

	prompt := strings.Join(os.Args[1:], " ")

	p := provider.NewOpenAI(key, "gpt-4o-mini")
	a := agent.New(p, "")

	reply, err := a.Reply(context.Background(), prompt)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(reply)
}
