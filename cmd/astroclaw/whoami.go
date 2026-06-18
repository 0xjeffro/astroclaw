package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"text/tabwriter"

	"astroclaw/pkg/client"

	"github.com/spf13/cobra"
)

func whoamiCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "whoami",
		Short: "Print the currently authenticated user",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			me, err := c.Me(cmd.Context())
			if err != nil {
				if errors.Is(err, client.ErrUnauthorized) {
					return fmt.Errorf("not logged in (run 'astroclaw login' first)")
				}
				return err
			}
			if asJSON {
				return json.NewEncoder(os.Stdout).Encode(me)
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintf(tw, "ID\t%s\n", me.ID)
			_, _ = fmt.Fprintf(tw, "Email\t%s\n", me.Email)
			_, _ = fmt.Fprintf(tw, "Name\t%s\n", me.Name)
			_, _ = fmt.Fprintf(tw, "Role\t%s\n", me.Role)
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}
