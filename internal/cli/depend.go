package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/app"
)

func newDependCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "depend",
		Short: "Manage task dependencies",
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
		Short: "Add a task dependency",
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
			ctx = rt.WithActivityRepo(ctx)

			project, err := opts.resolveProject(ctx, rt.store)
			if err != nil {
				return nil, err
			}
			dependency, err := app.NewDependencyServiceWithEvents(rt.store, rt.store).Add(ctx, project, taskID, on)
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "dependency": dependency}, nil
			})
		},
	}
	cmd.Flags().Int64VarP(&on, "on", "i", 0, "dependency task id")
	_ = cmd.MarkFlagRequired("on")
	return cmd
}

func newDependRemoveCommand(opts *runtimeOptions) *cobra.Command {
	var on int64
	cmd := &cobra.Command{
		Use:   "remove TASK_ID --on DEPENDS_ON_TASK_ID",
		Short: "Remove a task dependency",
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
			ctx = rt.WithActivityRepo(ctx)

			project, err := opts.resolveProject(ctx, rt.store)
			if err != nil {
				return nil, err
			}
			if err := app.NewDependencyServiceWithEvents(rt.store, rt.store).Remove(ctx, project, taskID, on); err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "removed": true}, nil
			})
		},
	}
	cmd.Flags().Int64VarP(&on, "on", "i", 0, "dependency task id")
	_ = cmd.MarkFlagRequired("on")
	return cmd
}

func newDependListCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list TASK_ID",
		Short: "List dependencies for a task",
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
				ctx = rt.WithActivityRepo(ctx)

				project, err := opts.resolveProject(ctx, rt.store)
				if err != nil {
					return nil, err
				}
				dependencies, err := app.NewDependencyService(rt.store).List(ctx, project, taskID)
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "dependencies": dependencies}, nil
			})
		},
	}
	return cmd
}
