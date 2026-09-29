package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/contract"
)

func newDependCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "depend",
		Short: opts.t("cli.depend.short"),
	}

	cmd.AddCommand(newDependAddCommand(opts))
	cmd.AddCommand(newDependRemoveCommand(opts))
	cmd.AddCommand(newDependListCommand(opts))
	return cmd
}

func newDependAddCommand(opts *runtimeOptions) *cobra.Command {
	var on int64
	cmd := &cobra.Command{
		Use:   "add TASK_ID --on DEPENDS_ON_TASK_ID",
		Short: opts.t("cli.depend.add.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				taskID, err := parseID(args[0], "task id")
				if err != nil {
					return nil, err
				}
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().AddDependency(ctx, contract.AddDependencyInput{
					ProjectSelector: opts.projectSelector(),
					TaskID:          taskID,
					DependsOnTaskID: on,
				})
			})
		},
	}
	cmd.Flags().Int64VarP(&on, "on", "i", 0, opts.t("cli.depend.flag.on"))
	_ = cmd.MarkFlagRequired("on")
	return cmd
}

func newDependRemoveCommand(opts *runtimeOptions) *cobra.Command {
	var on int64
	var confirm bool
	cmd := &cobra.Command{
		Use:   "remove TASK_ID --on DEPENDS_ON_TASK_ID",
		Short: opts.t("cli.depend.remove.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				taskID, err := parseID(args[0], "task id")
				if err != nil {
					return nil, err
				}
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().RemoveDependency(ctx, contract.RemoveDependencyInput{
					ProjectSelector: opts.projectSelector(),
					TaskID:          taskID,
					DependsOnTaskID: on,
					Confirmed:       confirm,
				})
			})
		},
	}
	cmd.Flags().Int64VarP(&on, "on", "i", 0, opts.t("cli.depend.flag.on"))
	cmd.Flags().BoolVar(&confirm, "confirm", false, opts.t("cli.depend.remove.flag.confirm"))
	_ = cmd.MarkFlagRequired("on")
	return cmd
}

func newDependListCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list TASK_ID",
		Short: opts.t("cli.depend.list.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				taskID, err := parseID(args[0], "task id")
				if err != nil {
					return nil, err
				}
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().ListDependencies(ctx, contract.ListDependenciesInput{
					ProjectSelector: opts.projectSelector(),
					TaskID:          taskID,
				})
			})
		},
	}
	return cmd
}
