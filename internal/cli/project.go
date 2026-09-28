package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/operation"
)

func newProjectCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: opts.t("cli.project.short"),
	}
	cmd.AddCommand(newProjectOverviewCommand(opts))
	cmd.AddCommand(newProjectResumeCommand(opts))
	cmd.AddCommand(newProjectEditCommand(opts))
	return cmd
}

func (o *runtimeOptions) projectSelector() operation.ProjectSelector {
	return operation.ProjectSelector{Project: o.project, ProjectID: o.projectID}
}

func newProjectOverviewCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "overview",
		Short: opts.t("cli.project.overview.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().Overview(ctx, operation.OverviewInput{
					ProjectSelector: opts.projectSelector(),
				})
			})
		},
	}
}

func newProjectResumeCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "resume",
		Short: opts.t("cli.project.resume.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().ResumeProject(ctx, operation.ResumeProjectInput{
					ProjectSelector: opts.projectSelector(),
				})
			})
		},
	}
}

func newProjectEditCommand(opts *runtimeOptions) *cobra.Command {
	var description string
	cmd := &cobra.Command{
		Use:   "edit",
		Short: opts.t("cli.project.edit.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().EditProject(ctx, operation.EditProjectInput{
					ProjectSelector: opts.projectSelector(),
					Description:     description,
				})
			})
		},
	}
	cmd.Flags().StringVar(&description, "description", "", opts.t("cli.project.edit.flag.description"))
	_ = cmd.MarkFlagRequired("description")
	return cmd
}
