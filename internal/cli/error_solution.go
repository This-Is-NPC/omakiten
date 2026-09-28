package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/domain"
	"omakiten/internal/operation"
)

func newErrorCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "error",
		Short: opts.t("cli.error.short"),
	}
	cmd.AddCommand(newErrorRecordCommand(opts))
	return cmd
}

func newErrorRecordCommand(opts *runtimeOptions) *cobra.Command {
	var description, errContext string
	var tags []string
	cmd := &cobra.Command{
		Use:   "record",
		Short: opts.t("cli.error.record.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().RecordError(ctx, operation.RecordErrorInput{
					ProjectSelector: opts.projectSelector(),
					Description:     description,
					Context:         errContext,
					Tags:            tags,
				})
			})
		},
	}
	cmd.Flags().StringVar(&description, "description", "", opts.t("cli.error.record.flag.description"))
	cmd.Flags().StringVar(&errContext, "context", "", opts.t("cli.error.record.flag.context"))
	cmd.Flags().StringArrayVar(&tags, "tag", nil, opts.t("cli.error.record.flag.tag"))
	_ = cmd.MarkFlagRequired("description")
	return cmd
}

func newSolutionCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "solution",
		Short: opts.t("cli.solution.short"),
	}
	cmd.AddCommand(newSolutionAddCommand(opts))
	cmd.AddCommand(newSolutionConfirmCommand(opts))
	cmd.AddCommand(newSolutionListTopCommand(opts))
	return cmd
}

func newSolutionAddCommand(opts *runtimeOptions) *cobra.Command {
	var errorID, taskID int64
	var description, steps string
	cmd := &cobra.Command{
		Use:   "add",
		Short: opts.t("cli.solution.add.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().AddSolution(ctx, operation.AddSolutionInput{
					ProjectSelector: opts.projectSelector(),
					ErrorID:         errorID,
					Description:     description,
					Steps:           steps,
					TaskID:          taskID,
				})
			})
		},
	}
	cmd.Flags().Int64Var(&errorID, "error-id", 0, opts.t("cli.solution.add.flag.error-id"))
	cmd.Flags().StringVar(&description, "description", "", opts.t("cli.solution.add.flag.description"))
	cmd.Flags().StringVar(&steps, "steps", "", opts.t("cli.solution.add.flag.steps"))
	cmd.Flags().Int64Var(&taskID, "task-id", 0, opts.t("cli.solution.add.flag.task-id"))
	_ = cmd.MarkFlagRequired("error-id")
	_ = cmd.MarkFlagRequired("description")
	return cmd
}

func newSolutionConfirmCommand(opts *runtimeOptions) *cobra.Command {
	var solutionID int64
	var success bool
	cmd := &cobra.Command{
		Use:   "confirm",
		Short: opts.t("cli.solution.confirm.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				if !cmd.Flags().Changed("success") {
					return nil, domain.NewError(domain.ErrValidation, opts.t("cli.solution.confirm.err.success_required"), nil)
				}
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().ConfirmSolution(ctx, operation.ConfirmSolutionInput{
					ProjectSelector: opts.projectSelector(),
					SolutionID:      solutionID,
					Success:         success,
				})
			})
		},
	}
	cmd.Flags().Int64Var(&solutionID, "solution-id", 0, opts.t("cli.solution.confirm.flag.solution-id"))
	cmd.Flags().BoolVar(&success, "success", false, opts.t("cli.solution.confirm.flag.success"))
	_ = cmd.MarkFlagRequired("solution-id")
	return cmd
}

func newSolutionListTopCommand(opts *runtimeOptions) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "list-top",
		Short: opts.t("cli.solution.list_top.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().ListTopSolutions(ctx, operation.ListTopSolutionsInput{
					ProjectSelector: opts.projectSelector(),
					Limit:           limit,
				})
			})
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, opts.t("cli.solution.list_top.flag.limit"))
	return cmd
}
