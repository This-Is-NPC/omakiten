package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/contract"
)

func newTaskCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: opts.t("cli.task.short"),
	}
	cmd.AddCommand(newTaskShowCommand(opts))
	cmd.AddCommand(newTaskContinueCommand(opts))
	cmd.AddCommand(newTaskCreateCommand(opts))
	cmd.AddCommand(newTaskActivityCommand(opts))
	cmd.AddCommand(newWorkImportCommand(opts, "task"))
	cmd.AddCommand(newWorkExportCommand(opts, "task"))
	return cmd
}

func newTaskShowCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "show TASK_ID",
		Short: opts.t("cli.task.show.short"),
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

				return rt.operationService().ShowTask(ctx, contract.ShowTaskInput{
					ProjectSelector: opts.projectSelector(),
					TaskID:          taskID,
				})
			})
		},
	}
}

func newTaskContinueCommand(opts *runtimeOptions) *cobra.Command {
	var includeWorkflow bool
	cmd := &cobra.Command{
		Use:   "continue TASK_ID",
		Short: opts.t("cli.task.continue.short"),
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

				input := contract.ContinueTaskInput{
					ProjectSelector: opts.projectSelector(),
					TaskID:          taskID,
				}
				if cmd.Flags().Changed("include-workflow") {
					input.IncludeWorkflow = &includeWorkflow
				}
				return rt.operationService().ContinueTask(ctx, input)
			})
		},
	}
	cmd.Flags().BoolVar(&includeWorkflow, "include-workflow", true, opts.t("cli.task.continue.flag.include-workflow"))
	return cmd
}

func newTaskActivityCommand(opts *runtimeOptions) *cobra.Command {
	var order string
	cmd := &cobra.Command{
		Use:   "activity TASK_ID",
		Short: opts.t("cli.task.activity.short"),
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

				return rt.operationService().ListTaskActivity(ctx, contract.ListTaskActivityInput{
					ProjectSelector: opts.projectSelector(),
					TaskID:          taskID,
					Order:           order,
				})
			})
		},
	}
	cmd.Flags().StringVar(&order, "order", "", opts.t("cli.task.activity.flag.order"))
	return cmd
}
