package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/contract"
)

func newAgentCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "agent", Short: opts.t("cli.agent.short")}
	cmd.AddCommand(newAgentSpawnCommand(opts), newAgentShowCommand(opts))
	return cmd
}

func newAgentSpawnCommand(opts *runtimeOptions) *cobra.Command {
	var taskID int64
	var input contract.SpawnAgentsInput
	cmd := &cobra.Command{
		Use:   "spawn --task ID [--command C] [--harness H] [--model M] [--launcher L]",
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
	cmd.Flags().StringVar(&input.Command, "command", "", opts.t("cli.agent.spawn.flag.command"))
	cmd.Flags().StringVar(&input.Harness, "harness", "", opts.t("cli.agent.spawn.flag.harness"))
	cmd.Flags().StringVar(&input.Model, "model", "", opts.t("cli.agent.spawn.flag.model"))
	cmd.Flags().StringVar(&input.Launcher, "launcher", "", opts.t("cli.agent.spawn.flag.launcher"))
	_ = cmd.MarkFlagRequired("task")
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
