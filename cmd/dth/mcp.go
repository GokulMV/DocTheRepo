package main

import (
	"context"
	"errors"

	"github.com/spf13/cobra"

	"github.com/GokulMV/DocTheRepo/internal/mcp"
)

// mcpCmd serves the Hub to coding agents (opencode, Claude Code, Cursor, ...) over MCP on stdio.
func (a *app) mcpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Serve the Hub as an MCP server on stdio (for opencode, Claude Code, Cursor, ...)",
		Long: "Runs a Model Context Protocol server on stdin/stdout. Tools: ask, search_entities, entity_graph,\n" +
			"list_issues, get_issue, list_known_issues, library, read_doc — all read-only, called with your token\n" +
			"(dth login, or DTH_SERVER + DTH_TOKEN), so your repository access applies.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if a.token == "" {
				return errors.New("not signed in: run `dth login --server <url> --token <token>` or set DTH_SERVER and DTH_TOKEN")
			}
			c := a.client()
			s := &mcp.Server{Name: "doctherepo-hub", Version: version, Instructions: mcp.Instructions, Log: a.errOut,
				Tools: mcp.HubTools(func(ctx context.Context, method, path string, body, out any) error {
					return c.call(ctx, method, path, body, out)
				})}
			err := s.Serve(cmd.Context(), cmd.InOrStdin(), a.out)
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		},
	}
}
