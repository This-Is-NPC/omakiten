package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/contract"
)

// newDeleteCommand wires `okt delete TASK_ID [--confirm]` against
// operation.Service.DeleteTask. Without --confirm the facade returns a
// Confirmation block (matching tag remove / agent); with --confirm the hard
// delete runs and bucket policy / operations.delete.guards still apply.
func newDeleteCommand(opts *runtimeOptions) *cobra.Command {
	var confirmed bool
	cmd := &cobra.Command{
		Use:   "delete TASK_ID",
		Short: opts.t("cli.task.delete.short"),
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

				return rt.operationService().DeleteTask(ctx, contract.DeleteTaskInput{
					ProjectSelector: opts.projectSelector(),
					TaskID:          taskID,
					Confirmed:       confirmed,
				})
			})
		},
	}
	cmd.Flags().BoolVar(&confirmed, "confirm", false, opts.t("cli.task.delete.flag.confirm"))
	return cmd
}

func newArchiveCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "archive TASK_ID",
		Short: opts.t("cli.task.archive.short"),
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

				return rt.operationService().ArchiveTask(ctx, contract.ArchiveTaskInput{
					ProjectSelector: opts.projectSelector(),
					TaskID:          taskID,
				})
			})
		},
	}
	return cmd
}

func newUnarchiveCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unarchive TASK_ID",
		Short: opts.t("cli.task.unarchive.short"),
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

				return rt.operationService().UnarchiveTask(ctx, contract.ArchiveTaskInput{
					ProjectSelector: opts.projectSelector(),
					TaskID:          taskID,
				})
			})
		},
	}
	return cmd
}
