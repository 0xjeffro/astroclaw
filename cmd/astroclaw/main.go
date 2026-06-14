package main

import (
	"astroclaw/pkg/client"
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "astroclaw",
		Short: "Astroclaw CLI",
	}

	rootCmd.AddCommand(loginCmd(), workspacesCmd())

	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		os.Exit(1)
	}
}

func newClient() (*client.Client, error) {
	apiURL := os.Getenv("ASTROCLAW_API_URL")
	if apiURL == "" {
		return nil, fmt.Errorf("ASTROCLAW_API_URL not set")
	}
	c := client.New(apiURL)
	if tok := os.Getenv("ASTROCLAW_TOKEN"); tok != "" {
		c.SetToken(tok)
	}
	return c, nil
}
