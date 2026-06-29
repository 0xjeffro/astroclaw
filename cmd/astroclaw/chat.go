package main

import (
	"errors"
	"fmt"
	"os"

	"astroclaw/pkg/app/settings"
	"astroclaw/pkg/client"

	"github.com/spf13/cobra"
)

func chatCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "chat <message>",
		Short: "Send a message to the default agent and print the reply",
		Long: `Chat finds your first workspace, looks up its default agent, creates a
fresh session, sends the message, and prints the agent's reply.

Requires both ASTROCLAW_API_URL and ASTROCLAW_REPLY_URL to be set
(see the deploy script output for both URLs).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			text := args[0]

			replyURL := os.Getenv("ASTROCLAW_REPLY_URL")
			if replyURL == "" {
				return fmt.Errorf("ASTROCLAW_REPLY_URL not set")
			}

			c, err := newClient()
			if err != nil {
				return err
			}
			c.SetReplyURL(replyURL)

			ctx := cmd.Context()

			me, err := c.Me(ctx)
			if err != nil {
				if errors.Is(err, client.ErrUnauthorized) {
					return fmt.Errorf("not logged in (run 'astroclaw login' first)")
				}
				return fmt.Errorf("get current user: %w", err)
			}

			ws, err := c.ListUserWorkspaces(ctx, me.ID)
			if err != nil {
				return fmt.Errorf("list workspaces: %w", err)
			}
			if len(ws) == 0 {
				return fmt.Errorf("user has no workspaces")
			}
			workspace := ws[0]

			setting, err := c.GetWorkspaceSetting(ctx, workspace.ID, settings.SettingWorkspaceDefaultAgentID)
			if err != nil {
				return fmt.Errorf("get default agent for workspace %s: %w", workspace.ID, err)
			}
			agentID := setting.Value

			session, err := c.CreateSession(ctx, workspace.ID, me.ID, []string{agentID}, "e2e-chat")
			if err != nil {
				return fmt.Errorf("create session: %w", err)
			}

			reply, err := c.Chat(ctx, session.ID, agentID, text)
			if err != nil {
				return fmt.Errorf("send message: %w", err)
			}

			fmt.Println(reply)
			return nil
		},
	}
	return cmd
}
