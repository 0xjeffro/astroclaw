package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func loginCmd() *cobra.Command {
	var email, password string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in and print the bearer token to stdout",
		Long: `Log in with email and password. The token is printed to stdout so it
can be captured into an environment variable, for example:

	export ASTROCLAW_TOKEN=$(astroclaw login --email me@example.com)

The password is read from the --password flag, then the ASTROCLAW_PASSWORD
environment variable, then an interactive prompt (in that order).

Status messages such as the password prompt go to stderr, leaving stdout
clean for shell substitution.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if email == "" {
				return fmt.Errorf("--email is required")
			}
			// Password resolution order: --password flag, then
			// ASTROCLAW_PASSWORD env var, then interactive prompt.
			if password == "" {
				password = os.Getenv("ASTROCLAW_PASSWORD")
			}
			if password == "" {
				_, _ = fmt.Fprint(os.Stderr, "password: ")
				b, err := term.ReadPassword(int(os.Stdin.Fd()))
				_, _ = fmt.Fprintln(os.Stderr)
				if err != nil {
					return fmt.Errorf("read password: %w", err)
				}
				password = string(b)
			}
			c, err := newClient()
			if err != nil {
				return err
			}
			resp, err := c.Login(cmd.Context(), email, password)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(os.Stderr, "Logged in as %s\n", resp.User.Email)
			fmt.Println(resp.Token)
			return nil
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "account email")
	cmd.Flags().StringVar(&password, "password", "", "account password (omit to prompt)")
	return cmd
}
