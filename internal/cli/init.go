package cli

import (
	"context"
	"os"

	"github.com/spf13/cobra"

	"omakiten/internal/agentsetup"
	"omakiten/internal/app"
)

func newInitCommand(opts *runtimeOptions) *cobra.Command {
	var name string
	var slug string
	var root string
	var enableMCP bool
	var mcpHarness string
	var mcpConfigPath string
	var mcpCommand string
	var mcpDryRun bool
	var mcpForce bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Register the current project in the global database",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
			rt, err := opts.open(ctx, true)
			if err != nil {
				return nil, err
			}
			defer func() { _ = rt.store.Close() }()
			ctx = rt.WithActivityRepo(ctx)

			projectRoot := root
				if projectRoot == "" {
					projectRoot, err = os.Getwd()
					if err != nil {
						return nil, err
					}
				}

				project, err := app.NewProjectService(rt.store).Init(ctx, name, slug, projectRoot)
				if err != nil {
					return nil, err
				}

				result := map[string]any{"project": project, "db_path": rt.dbPath, "config_path": rt.configPath}
				if enableMCP {
					setup, err := agentsetup.Setup(agentsetup.Options{Harness: mcpHarness, ConfigPath: mcpConfigPath, Command: mcpCommand, DryRun: mcpDryRun, Force: mcpForce})
					if err != nil {
						return nil, err
					}
					result["agent_setup"] = setup
				}
				return result, nil
			})
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "project name")
	cmd.Flags().StringVar(&slug, "slug", "", "project slug")
	cmd.Flags().StringVar(&root, "root", "", "project root path")
	cmd.Flags().BoolVar(&enableMCP, "enable-mcp", false, "enable global MCP agent access for a supported harness")
	cmd.Flags().StringVar(&mcpHarness, "mcp-harness", agentsetup.ClaudeDesktopHarness, "MCP harness to configure")
	cmd.Flags().StringVar(&mcpConfigPath, "mcp-config", "", "MCP harness config path")
	cmd.Flags().StringVar(&mcpCommand, "mcp-command", "", "command path written to the harness MCP config")
	cmd.Flags().BoolVar(&mcpDryRun, "mcp-dry-run", false, "preview MCP harness config changes without writing")
	cmd.Flags().BoolVar(&mcpForce, "mcp-force", false, "replace an existing Omakiten MCP harness entry")
	return cmd
}
