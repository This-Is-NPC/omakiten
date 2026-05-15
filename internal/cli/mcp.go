package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"omakiten/internal/agent"
	"omakiten/internal/agentruntime"
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
	cmd.AddCommand(newMCPPromptsCommand(opts))
	return cmd
}

// newMCPPromptsCommand renders each `okt-*` prompt's resolved markdown to
// stdout so users can preview what the agent receives without spinning up
// an MCP client. With no argument, every prompt in `agent.CommandNames()` is
// rendered in handoff order, separated by horizontal rules and annotated
// with byte/rune counts. A single name argument renders that prompt only.
func newMCPPromptsCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "prompts [name]",
		Short: "Render resolved okt-* prompt markdown to stdout",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			rt, err := agentruntime.Open(ctx, agentOptions(opts))
			if err != nil {
				return err
			}
			defer func() { _ = rt.Close() }()

			adapter := newMCPAdapter(rt)

			names := agent.CommandNames()
			if len(args) == 1 {
				names = []string{args[0]}
			}

			out := cmd.OutOrStdout()
			for i, name := range names {
				if i > 0 {
					fmt.Fprint(out, "\n---\n\n")
				}
				result, err := adapter.GetPrompt(ctx, name, nil)
				if err != nil {
					return fmt.Errorf("get prompt %s: %w", name, err)
				}
				if len(result.Messages) == 0 {
					return fmt.Errorf("prompt %s returned no messages", name)
				}
				body := result.Messages[0].Content.Text
				fmt.Fprintf(out, "# %s — %d bytes / %d runes\n\n%s\n", name, len(body), runeCount(body), body)
			}
			return nil
		},
	}
}

func runeCount(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
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

				rt, err := agentruntime.Open(ctx, agentOptions(opts))
				if err != nil {
					return nil, err
				}
				defer func() { _ = rt.Close() }()

				adapter := newMCPAdapter(rt)
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
			rt, err := agentruntime.Open(cmd.Context(), agentOptions(opts))
			if err != nil {
				return err
			}
			defer func() { _ = rt.Close() }()
			adapter := newMCPAdapter(rt)
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

	cmd.Flags().StringVar(&harness, "harness", agentsetup.ClaudeCodeHarness, "Target harness ("+strings.Join(agentsetup.SupportedHarnesses(), ", ")+")")
	cmd.Flags().StringVar(&configPath, "config-path", "", "Path to harness config file (default: harness default)")
	cmd.Flags().StringVar(&command, "command", "", "Command to run omakiten (default: current executable)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview changes without writing")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing Omakiten MCP config")

	return cmd
}

func agentOptions(opts *runtimeOptions) agentruntime.Options {
	cwd, _ := os.Getwd()
	return agentruntime.Options{DBPath: opts.dbPath, ConfigPath: opts.configPath, Project: opts.project, ProjectID: opts.projectID, CWD: cwd}
}

// newMCPAdapter is the single place that builds a configured MCP
// adapter for the CLI command surfaces. It wires the per-project
// ServiceResolver against the runtime's BundleCache so MCP tool calls
// carrying `project` / `project_id` route to the right ProjectRuntime.
// Calls without a project arg keep using the default service (the
// runtime's boot-time selector), matching the pre-3b single-project
// behaviour.
func newMCPAdapter(rt *agentruntime.Runtime) *mcp.Adapter {
	adapter := mcp.NewAdapter(rt.Service())
	adapter.SetActivityLogRepository(rt.Store())
	adapter.SetServiceResolver(func(ctx context.Context, project string, projectID int64) (*agent.Service, error) {
		return rt.ResolveServiceForProject(ctx, project, projectID)
	})
	return adapter
}
