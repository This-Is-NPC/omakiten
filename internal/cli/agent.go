package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"omakiten/internal/contract"
)

func newAgentCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "agent", Short: opts.t("cli.agent.short")}
	cmd.AddCommand(newAgentSpawnCommand(opts), newAgentShowCommand(opts), newAgentListCommand(opts), newAgentRunCommand(opts))
	return cmd
}

func newAgentListCommand(opts *runtimeOptions) *cobra.Command {
	var input contract.ListAgentsInput
	cmd := &cobra.Command{
		Use:   "list [--task ID] [--status active|failed|closed]",
		Short: opts.t("cli.agent.list.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				input.ProjectSelector = opts.projectSelector()
				return rt.operationService().ListAgents(ctx, input)
			})
		},
	}
	cmd.Flags().Int64Var(&input.TaskID, "task", 0, opts.t("cli.agent.list.flag.task"))
	cmd.Flags().StringVar(&input.Status, "status", "", opts.t("cli.agent.list.flag.status"))
	return cmd
}

func newAgentSpawnCommand(opts *runtimeOptions) *cobra.Command {
	var taskID int64
	var input contract.SpawnAgentsInput
	cmd := &cobra.Command{
		Use:   "spawn (--task ID [--command C] | --team T --member KEY | --plan SLUG --member KEY) [--harness H] [--model M]",
		Short: opts.t("cli.agent.spawn.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				input.ProjectSelector = opts.projectSelector()
				input.TaskID = taskID
				return rt.operationService().SpawnAgents(ctx, input)
			})
		},
	}
	cmd.Flags().Int64Var(&taskID, "task", 0, opts.t("cli.agent.spawn.flag.task"))
	cmd.Flags().StringVar(&input.TeamID, "team", "", opts.t("cli.agent.spawn.flag.team"))
	cmd.Flags().StringVar(&input.Plan, "plan", "", opts.t("cli.agent.spawn.flag.plan"))
	cmd.Flags().StringVar(&input.Member, "member", "", opts.t("cli.agent.spawn.flag.member"))
	cmd.Flags().StringVar(&input.Command, "command", "", opts.t("cli.agent.spawn.flag.command"))
	cmd.Flags().StringVar(&input.Harness, "harness", "", opts.t("cli.agent.spawn.flag.harness"))
	cmd.Flags().StringVar(&input.Model, "model", "", opts.t("cli.agent.spawn.flag.model"))
	cmd.MarkFlagsMutuallyExclusive("task", "team", "plan")
	cmd.MarkFlagsOneRequired("task", "team", "plan")
	return cmd
}

// newAgentRunCommand is the supervisor `agent spawn` starts detached for
// each seat; a stop signal ends the harness it runs.
func newAgentRunCommand(opts *runtimeOptions) *cobra.Command {
	var input contract.RunAgentSessionInput
	cmd := &cobra.Command{
		Use:    "run --session ID [--timeout D] -- HARNESS_ARGV...",
		Short:  opts.t("cli.agent.run.short"),
		Hidden: true,
		Args:   cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
				defer stop()
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				input.ProjectSelector = opts.projectSelector()
				input.Argv = args
				return rt.operationService().RunAgentSession(ctx, input)
			})
		},
	}
	cmd.Flags().StringVar(&input.SessionID, "session", "", opts.t("cli.agent.run.flag.session"))
	cmd.Flags().DurationVar(&input.Timeout, "timeout", time.Duration(0), opts.t("cli.agent.run.flag.timeout"))
	_ = cmd.MarkFlagRequired("session")
	return cmd
}

func newAgentShowCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "show SESSION_ID",
		Short: opts.t("cli.agent.show.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().ShowAgent(ctx, contract.ShowAgentInput{ProjectSelector: opts.projectSelector(), SessionID: args[0]})
			})
		},
	}
}

func newTeamCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "team", Short: opts.t("cli.team.short")}
	cmd.AddCommand(&cobra.Command{
		Use:   "close TEAM_ID",
		Short: opts.t("cli.team.close.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().CloseTeam(ctx, contract.CloseTeamInput{ProjectSelector: opts.projectSelector(), TeamID: args[0]})
			})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "show TEAM_ID",
		Short: opts.t("cli.team.show.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().ShowTeam(ctx, contract.ShowTeamInput{ProjectSelector: opts.projectSelector(), TeamID: args[0]})
			})
		},
	})
	return cmd
}
