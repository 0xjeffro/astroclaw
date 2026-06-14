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

func workspacesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspaces",
		Short: "Manage workspaces",
	}
	cmd.AddCommand(workspacesListCmd())
	return cmd
}

func workspacesListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List your workspaces",
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
			ws, err := c.ListUserWorkspaces(cmd.Context(), me.ID)
			if err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(os.Stdout).Encode(ws)
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "ID\tNAME\tCREATED")
			for _, w := range ws {
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", w.ID, w.Name, w.CreatedAt.Format("2006-01-02"))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}
