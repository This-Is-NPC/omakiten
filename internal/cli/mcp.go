package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
		Short: opts.t("cli.mcp.short"),
	}

	cmd.AddCommand(newMCPToolsCommand(opts))
	cmd.AddCommand(newMCPCallCommand(opts))
	cmd.AddCommand(newMCPServeCommand(opts))
	cmd.AddCommand(newMCPSetupCommand(opts))
	cmd.AddCommand(newMCPPromptsCommand(opts))
	return cmd
}

// newMCPPromptsCommand renders each `okt-*` prompt's resolved markdown to
// stdout so users can preview what the agent receives without spinning up
// an MCP client. With no argument, every prompt in `agent.CommandNames()` is
// rendered in handoff order, separated by horizontal rules and annotated
// with byte/rune counts. A single name argument renders that prompt only.
//
// `--list` switches to the command-surface listing: instead of rendering
// bodies, it prints the 40-command kit grouped by routing tier (orchestrator /
// system / granular) with the granular tier further grouped by object
// namespace (`okt-<object>-<verb>`). This is the CLI's view of the same surface
// the MCP `prompts/list` exposes, so users can see the namespacing and tiers
// from the shell without an MCP client.
func newMCPPromptsCommand(opts *runtimeOptions) *cobra.Command {
	var list bool
	cmd := &cobra.Command{
		Use:   "prompts [name]",
		Short: opts.t("cli.mcp.prompt.short"),
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if list {
				return printPromptSurface(cmd.OutOrStdout(), opts)
			}

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
				fmt.Fprintf(out, opts.t("cli.print.prompt_render"), name, len(body), runeCount(body), body)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&list, "list", false, opts.t("cli.mcp.prompt.flag.list"))
	return cmd
}

// printPromptSurface renders the command-surface listing: the full
// agent.CommandNames() kit grouped by routing tier, with the granular tier
// sub-grouped by object namespace. Order within each tier follows
// CommandNames() (the REST-style handoff order) so the listing reads the way a
// user invokes the commands.
func printPromptSurface(out io.Writer, opts *runtimeOptions) error {
	names := agent.CommandNames()

	var orchestrators, system []string
	granular := map[string][]string{}
	var objectOrder []string

	for _, name := range names {
		desc, ok := agent.DescribeCommand(name)
		if !ok {
			// A registered command that does not decode is a surface bug; surface
			// it loudly rather than silently dropping it from the listing.
			return fmt.Errorf("command %q does not decode into a known tier — the surface and the registry disagree", name)
		}
		switch desc.Tier {
		case agent.CommandTierOrchestrator:
			orchestrators = append(orchestrators, name)
		case agent.CommandTierSystem:
			system = append(system, name)
		case agent.CommandTierGranular:
			if _, seen := granular[desc.Object]; !seen {
				objectOrder = append(objectOrder, desc.Object)
			}
			granular[desc.Object] = append(granular[desc.Object], name)
		}
	}

	fmt.Fprintf(out, opts.t("cli.mcp.prompt.list.title"), len(names))

	writeRow := func(indent, name string) {
		fmt.Fprintf(out, "%s%-22s %s\n", indent, name, agent.CommandDescription(name))
	}

	fmt.Fprintf(out, "\nOrchestrators (%d) — bare, director path\n", len(orchestrators))
	for _, name := range orchestrators {
		writeRow("  ", name)
	}

	fmt.Fprintf(out, "\nSystem (%d) — talk to the tool, no project object\n", len(system))
	for _, name := range system {
		writeRow("  ", name)
	}

	granularCount := 0
	for _, names := range granular {
		granularCount += len(names)
	}
	fmt.Fprintf(out, "\nGranular (%d) — okt-<object>-<verb>, surgical\n", granularCount)
	for _, object := range objectOrder {
		fmt.Fprintf(out, "  %s (%d)\n", object, len(granular[object]))
		for _, name := range granular[object] {
			writeRow("    ", name)
		}
	}
	return nil
}

func runeCount(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

func newMCPToolsCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "tools",
		Short: opts.t("cli.mcp.tools.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return writeSuccess(cmd, map[string]any{"tools": mcp.Tools(), "resources": mcp.Resources(), "prompts": mcp.Prompts()})
		},
	}
}

func newMCPCallCommand(opts *runtimeOptions) *cobra.Command {
	var inputJSON string
	cmd := &cobra.Command{
		Use:   "call TOOL_NAME",
		Short: opts.t("cli.mcp.call.short"),
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
	cmd.Flags().StringVar(&inputJSON, "input", "", opts.t("cli.mcp.call.flag.input"))
	return cmd
}

func newMCPServeCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: opts.t("cli.mcp.serve.short"),
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

func newMCPSetupCommand(opts *runtimeOptions) *cobra.Command {
	var harness, configPath, command string
	var dryRun, force bool

	cmd := &cobra.Command{
		Use:   "setup",
		Short: opts.t("cli.mcp.install.short"),
		Long:  opts.t("cli.mcp.install.long"),
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

	cmd.Flags().StringVar(&harness, "harness", agentsetup.ClaudeCodeHarness, fmt.Sprintf(opts.t("cli.mcp.install.flag.harness"), strings.Join(agentsetup.SupportedHarnesses(), ", ")))
	cmd.Flags().StringVar(&configPath, "config-path", "", opts.t("cli.mcp.install.flag.config-path"))
	cmd.Flags().StringVar(&command, "command", "", opts.t("cli.mcp.install.flag.command"))
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, opts.t("cli.mcp.install.flag.dry-run"))
	cmd.Flags().BoolVar(&force, "force", false, opts.t("cli.mcp.install.flag.force"))

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
	// Provider lookup keeps the default service fresh across cache
	// rebuilds — without it, a mtime-driven Reload during a long MCP
	// session would leave the adapter dispatching against a stale
	// agent.Service pointer.
	adapter.SetDefaultServiceProvider(rt.Service)
	adapter.SetActivityLogRepository(rt.Store())
	adapter.SetServiceResolver(func(ctx context.Context, project string, projectID int64) (*agent.Service, error) {
		return rt.ResolveServiceForProject(ctx, project, projectID)
	})
	return adapter
}
