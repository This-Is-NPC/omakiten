package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/app"
	"omakiten/internal/domain"
)

// newDeleteCommand wires `okt delete TASK_ID --confirm` against
// TaskService.Delete. The confirm flag is required to avoid accidental hard
// deletes; the service still enforces bucket policy and operations.delete.guards.
func newDeleteCommand(opts *runtimeOptions) *cobra.Command {
	var confirmed bool
	cmd := &cobra.Command{
		Use:   "delete TASK_ID",
		Short: "Hard-delete a task (cascade) — subject to bucket policy and operations.delete.guards",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				taskID, err := parseTaskID(args[0])
				if err != nil {
					return nil, err
				}
				if !confirmed {
					return nil, domain.NewError(domain.ErrValidation, "task delete requires --confirm to acknowledge the destructive cascade", map[string]any{"task_id": taskID, "hint": "consider okt archive instead for a reversible alternative"})
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
				event, err := app.NewTaskServiceFromStore(rt.store, rt.activeRegistry()).Delete(ctx, project, taskID)
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "snapshot": event}, nil
			})
		},
	}
	cmd.Flags().BoolVar(&confirmed, "confirm", false, "required: confirm the destructive delete")
	return cmd
}

func newArchiveCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "archive TASK_ID",
		Short: "Archive a task (state=archived, moves into the workflow's final bucket)",
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
				task, _, err := app.NewTaskServiceFromStore(rt.store, rt.activeRegistry()).Archive(ctx, project, taskID)
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "task": task}, nil
			})
		},
	}
	return cmd
}

func newUnarchiveCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unarchive TASK_ID",
		Short: "Restore an archived task (state=active)",
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
				task, _, err := app.NewTaskServiceFromStore(rt.store, rt.activeRegistry()).Unarchive(ctx, project, taskID)
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "task": task}, nil
			})
		},
	}
	return cmd
}
