package cli

import (
	"context"
	"strconv"

	"github.com/spf13/cobra"

	"omakiten/internal/domain"
	"omakiten/internal/operation"
)

func newTaskCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: opts.t("cli.task.short"),
	}
	cmd.AddCommand(newTaskContinueCommand(opts))
	cmd.AddCommand(newTaskCreateIntentCommand(opts))
	cmd.AddCommand(newTaskActivityCommand(opts))
	return cmd
}

func newTaskContinueCommand(opts *runtimeOptions) *cobra.Command {
	var includeWorkflow bool
	cmd := &cobra.Command{
		Use:   "continue TASK_ID",
		Short: opts.t("cli.task.continue.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				taskID, err := strconv.ParseInt(args[0], 10, 64)
				if err != nil {
					return nil, domain.NewError(domain.ErrValidation, "task id is not numeric", map[string]any{"value": args[0]})
				}
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				input := operation.ContinueTaskInput{
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

func newTaskCreateIntentCommand(opts *runtimeOptions) *cobra.Command {
	var (
		title        string
		description  string
		priority     string
		templateSlug string
		parentID     int64
		confirmed    bool
	)
	cmd := &cobra.Command{
		Use:   "create-intent",
		Short: opts.t("cli.task.create_intent.short"),
		Long:  opts.t("cli.task.create_intent.long"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				input := operation.CreateTaskInput{
					ProjectSelector: opts.projectSelector(),
					Title:           title,
					Description:     description,
					Priority:        priority,
					TemplateSlug:    templateSlug,
					Confirmed:       confirmed,
					// Never skip the similarity gate on this surface — that is
					// what distinguishes create-intent from okt add / CreateTask.
					SkipSimilarityCheck: false,
				}
				if cmd.Flags().Changed("parent") {
					input.ParentID = &parentID
				}
				return rt.operationService().CreateTaskIntent(ctx, input)
			})
		},
	}
	cmd.Flags().StringVarP(&title, "title", "t", "", opts.t("cli.task.create_intent.flag.title"))
	cmd.Flags().StringVarP(&description, "description", "d", "", opts.t("cli.task.create_intent.flag.description"))
	cmd.Flags().StringVar(&priority, "priority", "", opts.t("cli.task.create_intent.flag.priority"))
	cmd.Flags().StringVar(&templateSlug, "template", "", opts.t("cli.task.create_intent.flag.template"))
	cmd.Flags().Int64Var(&parentID, "parent", 0, opts.t("cli.task.create_intent.flag.parent"))
	cmd.Flags().BoolVar(&confirmed, "confirm", false, opts.t("cli.task.create_intent.flag.confirm"))
	_ = cmd.MarkFlagRequired("description")
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
				taskID, err := strconv.ParseInt(args[0], 10, 64)
				if err != nil {
					return nil, domain.NewError(domain.ErrValidation, "task id is not numeric", map[string]any{"value": args[0]})
				}
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				return rt.operationService().ListTaskActivity(ctx, operation.ListTaskActivityInput{
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
