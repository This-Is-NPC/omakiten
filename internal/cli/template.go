package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/operation"
)

func newTemplateCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "template",
		Short: opts.t("cli.template.short"),
	}
	cmd.AddCommand(newTemplateListCommand(opts))
	cmd.AddCommand(newTemplateShowCommand(opts))
	return cmd
}

func newTemplateListCommand(opts *runtimeOptions) *cobra.Command {
	var kind, project string
	var includeBody bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: opts.t("cli.template.list.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				return rt.operationService().ListTemplates(ctx, operation.ListTemplatesInput{
					Kind:        kind,
					Project:     project,
					IncludeBody: includeBody,
				})
			})
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", opts.t("cli.template.list.flag.kind"))
	cmd.Flags().StringVar(&project, "project", "", opts.t("cli.template.list.flag.project"))
	cmd.Flags().BoolVar(&includeBody, "include-body", false, opts.t("cli.template.list.flag.include-body"))
	return cmd
}

func newTemplateShowCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "show SLUG",
		Short: opts.t("cli.template.show.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				return rt.operationService().ShowTemplate(ctx, operation.ShowTemplateInput{
					ProjectSelector: operation.ProjectSelector{
						Project:   opts.project,
						ProjectID: opts.projectID,
					},
					Slug: args[0],
				})
			})
		},
	}
}
