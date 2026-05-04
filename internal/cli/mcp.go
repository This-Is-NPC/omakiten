package cli

import (
	"context"
	"encoding/json"
	"os"

	"github.com/spf13/cobra"

	"omakiten/internal/agent"
	"omakiten/internal/agentsetup"
	"omakiten/internal/mcp"
)

func newMCPCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Expose Omakiten agent intents through MCP",
	}

	cmd.AddCommand(newMCPToolsCommand())
	cmd.AddCommand(newMCPCallCommand(opts))
	cmd.AddCommand(newMCPServeCommand(opts))
	cmd.AddCommand(newMCPSetupCommand())
	return cmd
}

func newMCPToolsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "tools",
		Short: "List Omakiten MCP tool definitions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return writeSuccess(cmd, map[string]any{"tools": mcp.Tools(), "resources": mcp.Resources(), "prompts": mcp.Prompts()})
		},
	}
}

func newMCPCallCommand(opts *runtimeOptions) *cobra.Command {
	var inputJSON string
	cmd := &cobra.Command{
		Use:   "call TOOL_NAME",
		Short: "Call an Omakiten MCP tool with a JSON input object",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				var input map[string]any
				if inputJSON != "" {
					if err := json.Unmarshal([]byte(inputJSON), &input); err != nil {
						return nil, err
					}
				}

				rt, err := agent.Open(ctx, agentOptions(opts))
				if err != nil {
					return nil, err
				}
				defer func() { _ = rt.Close() }()

				adapter := mcp.NewAdapter(rt.Service())
				adapter.SetActivityLogRepository(rt.Store())
				result, err := adapter.CallTool(ctx, args[0], input)
				if err != nil {
					return nil, err
				}
				return map[string]any{"tool_result": result}, nil
			})
		},
	}
	cmd.Flags().StringVar(&inputJSON, "input", "", "JSON input object")
	return cmd
}

func newMCPServeCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the Omakiten MCP stdio server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, err := agent.Open(cmd.Context(), agentOptions(opts))
			if err != nil {
				return err
			}
			defer func() { _ = rt.Close() }()
			adapter := mcp.NewAdapter(rt.Service())
			adapter.SetActivityLogRepository(rt.Store())
			return mcp.Serve(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), adapter)
		},
	}
}

func newMCPSetupCommand() *cobra.Command {
	var harness, configPath, command string
	var dryRun, force bool

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure Omakiten MCP server in an AI harness",
		Long:  "Writes the Omakiten MCP server configuration to the harness config file (e.g. Claude Desktop or OpenCode).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := agentsetup.Setup(agentsetup.Options{
				Harness:    harness,
				ConfigPath: configPath,
				Command:    command,
				DryRun:     dryRun,
				Force:      force,
			})
			if err != nil {
				return writeError(cmd, err)
			}
			return writeSuccess(cmd, result)
		},
	}

	cmd.Flags().StringVar(&harness, "harness", "claude-desktop", "Target harness (claude-desktop or opencode)")
	cmd.Flags().StringVar(&configPath, "config-path", "", "Path to harness config file (default: harness default)")
	cmd.Flags().StringVar(&command, "command", "", "Command to run omakiten (default: current executable)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview changes without writing")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing Omakiten MCP config")

	return cmd
}

func agentOptions(opts *runtimeOptions) agent.Options {
	cwd, _ := os.Getwd()
	return agent.Options{DBPath: opts.dbPath, ConfigPath: opts.configPath, Project: opts.project, ProjectID: opts.projectID, CWD: cwd}
}
