package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/operation"
)

func newWorkflowCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflow",
		Short: opts.t("cli.workflow.short"),
	}

	show := &cobra.Command{
		Use:   "show",
		Short: opts.t("cli.workflow.show.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().ShowWorkflow(ctx, operation.WorkflowInput{
					ProjectSelector: opts.projectSelector(),
				})
			})
		},
	}

	cmd.AddCommand(show)
	cmd.AddCommand(newWorkflowOrphansCommand(opts))
	return cmd
}

func newWorkflowOrphansCommand(opts *runtimeOptions) *cobra.Command {
	var confirm bool
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "orphans",
		Short: opts.t("cli.workflow.orphan.short"),
		Long:  opts.t("cli.workflow.orphan.long"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				// dry-run keeps the preview path (--confirm ignored).
				return rt.operationService().MigrateOrphans(ctx, operation.MigrateOrphansInput{
					ProjectSelector: opts.projectSelector(),
					Confirmed:       confirm && !dryRun,
				})
			})
		},
	}

	cmd.Flags().BoolVar(&confirm, "confirm", false, opts.t("cli.workflow.orphan.flag.confirm"))
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, opts.t("cli.workflow.orphan.flag.dry-run"))
	return cmd
}
