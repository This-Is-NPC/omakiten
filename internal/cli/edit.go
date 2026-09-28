package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/operation"
)

// (parsePriority lives in enums.go for cross-command reuse.)

func newEditCommand(opts *runtimeOptions) *cobra.Command {
	var title string
	var description string
	var priority string
	var bucket string
	var parent int64

	cmd := &cobra.Command{
		Use:   "edit TASK_ID",
		Short: opts.t("cli.task.edit.short"),
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
				return runTaskEdit(ctx, cmd, opts, rt, taskID, title, description, priority, bucket, parent)
			})
		},
	}

	cmd.Flags().StringVarP(&title, "title", "t", "", opts.t("cli.task.edit.flag.title"))
	cmd.Flags().StringVarP(&description, "description", "d", "", opts.t("cli.task.edit.flag.description"))
	cmd.Flags().StringVar(&priority, "priority", "", opts.t("cli.task.edit.flag.priority"))
	cmd.Flags().StringVarP(&bucket, "bucket", "b", "", opts.t("cli.task.edit.flag.bucket"))
	cmd.Flags().Int64Var(&parent, "parent", 0, opts.t("cli.task.edit.flag.parent"))
	return cmd
}

func runTaskEdit(ctx context.Context, cmd *cobra.Command, opts *runtimeOptions, rt *runtime, taskID int64, title, description, priority, bucket string, parent int64) (any, error) {
	var response any
	if taskEditFieldsChanged(cmd) {
		input, err := editTaskInput(cmd, opts, rt, taskID, title, description, priority, parent)
		if err != nil {
			return nil, err
		}
		response, err = rt.operationService().EditTask(ctx, input)
		if err != nil {
			return nil, err
		}
	}
	if cmd.Flags().Changed("bucket") {
		// Bucket moves go through MoveTask so the activity log distinguishes
		// edit vs move (EditTask omits BucketKey).
		response, err := rt.operationService().MoveTask(ctx, operation.MoveTaskInput{
			ProjectSelector: opts.projectSelector(),
			TaskID:          taskID,
			BucketKey:       bucket,
		})
		if err != nil {
			return nil, err
		}
		return response, nil
	}
	if response != nil {
		return response, nil
	}
	// Mirror app.TaskService.Edit: empty patch is invalid.
	_, err := rt.operationService().EditTask(ctx, operation.EditTaskInput{
		ProjectSelector: opts.projectSelector(),
		TaskID:          taskID,
	})
	return nil, err
}

func taskEditFieldsChanged(cmd *cobra.Command) bool {
	return cmd.Flags().Changed("title") ||
		cmd.Flags().Changed("description") ||
		cmd.Flags().Changed("priority") ||
		cmd.Flags().Changed("parent")
}

func editTaskInput(cmd *cobra.Command, opts *runtimeOptions, rt *runtime, taskID int64, title, description, priority string, parent int64) (operation.EditTaskInput, error) {
	input := operation.EditTaskInput{ProjectSelector: opts.projectSelector(), TaskID: taskID}
	if cmd.Flags().Changed("title") {
		input.Title = &title
	}
	if cmd.Flags().Changed("description") {
		input.Description = &description
	}
	if cmd.Flags().Changed("priority") {
		value, err := parsePriority(priority, rt.activeRegistry())
		if err != nil {
			return operation.EditTaskInput{}, err
		}
		label := rt.activeRegistry().PriorityLabel(value)
		input.Priority = &label
	}
	if cmd.Flags().Changed("parent") {
		input.ParentID = operation.OptionalInt64{Set: true}
		if parent != 0 {
			input.ParentID.Value = &parent
		}
	}
	return input, nil
}
