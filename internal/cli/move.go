package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/contract"
)

func newMoveCommand(opts *runtimeOptions) *cobra.Command {
	var to string

	cmd := &cobra.Command{
		Use:   "move TASK_ID --to BUCKET",
		Short: opts.t("cli.task.move.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				taskID, err := parseTaskID(args[0])
				if err != nil {
					return nil, err
				}

				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				return rt.operationService().MoveTask(ctx, contract.MoveTaskInput{
					ProjectSelector: opts.projectSelector(),
					TaskID:          taskID,
					BucketKey:       to,
				})
			})
		},
	}

	cmd.Flags().StringVarP(&to, "to", "t", "", opts.t("cli.task.move.flag.to"))
	_ = cmd.MarkFlagRequired("to")
	return cmd
}
