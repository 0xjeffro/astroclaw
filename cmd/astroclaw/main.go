package main

import (
	"astroclaw/pkg/client"
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	os.Exit(run())
}

// Run does the actual work and returns an exit code. Separated from main
// so testscript can invoke it in-process without killing the test binary.
func run() int {
	rootCmd := &cobra.Command{
		Use:   "astroclaw",
		Short: "Astroclaw CLI",
	}
	rootCmd.AddCommand(loginCmd(), workspacesCmd(), whoamiCmd())

	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		return 1
	}
	return 0
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
