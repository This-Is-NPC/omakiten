package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/app"
	"omakiten/internal/domain"
)

func newEditCommand(opts *runtimeOptions) *cobra.Command {
	var title string
	var description string
	var priority string
	var bucket string

	cmd := &cobra.Command{
		Use:   "edit TASK_ID",
		Short: "Edit a task in the active project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				taskID, err := parseTaskID(args[0])
				if err != nil {
					return nil, err
				}

				update := domain.TaskUpdate{}
				if cmd.Flags().Changed("title") {
					update.Title = &title
				}
				if cmd.Flags().Changed("description") {
					update.Description = &description
				}
				if cmd.Flags().Changed("priority") {
					// CLI accepts the priority label string; resolve to
					// the configured id via the registry so the service
					// layer never sees raw strings. Unknown labels error
					// loudly instead of silently writing PriorityZero.
					value, ok := domain.PriorityFromLabel(priority)
					if !ok {
						return nil, domain.NewError(domain.ErrValidation,
							"unknown priority label; must match a value in config.priorities",
							map[string]any{"priority": priority})
					}
					update.Priority = &value
				}
				if cmd.Flags().Changed("bucket") {
					update.BucketKey = bucket
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
				task, err := app.NewTaskServiceFromStore(rt.store).Edit(ctx, project, taskID, update)
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "task": task}, nil
			})
		},
	}

	cmd.Flags().StringVarP(&title, "title", "t", "", "task title")
	cmd.Flags().StringVarP(&description, "description", "d", "", "task description")
	cmd.Flags().StringVar(&priority, "priority", "", "task priority: low, normal, or high")
	cmd.Flags().StringVarP(&bucket, "bucket", "b", "", "target bucket key")
	return cmd
}
