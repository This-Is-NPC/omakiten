package cli

import (
	"context"
	"strconv"

	"github.com/spf13/cobra"

	"omakiten/internal/app"
	"omakiten/internal/domain"
)

func newMoveCommand(opts *runtimeOptions) *cobra.Command {
	var to string

	cmd := &cobra.Command{
		Use:   "move TASK_ID --to BUCKET",
		Short: "Move a task through an allowed workflow transition",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				taskID, err := strconv.ParseInt(args[0], 10, 64)
				if err != nil {
					return nil, domain.NewError(domain.ErrValidation, "task id must be numeric", map[string]any{"value": args[0]})
				}

				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer func() { _ = rt.store.Close() }()

				project, err := opts.resolveProject(ctx, rt.store)
				if err != nil {
					return nil, err
				}

				task, err := app.NewTaskService(rt.store).Move(ctx, project, taskID, to)
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "task": task}, nil
			})
		},
	}

	cmd.Flags().StringVarP(&to, "to", "t", "", "target bucket key")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}
