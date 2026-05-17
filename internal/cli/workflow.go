package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/app"
	"omakiten/internal/domain"
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
				// Workflow read is a pure config lookup served from
				// the in-memory Snapshot; no activity tracking
				// needed since nothing touches the DB.
				return map[string]any{"workflow": rt.activeSnapshot().Workflow()}, nil
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
		Long: opts.t("cli.workflow.orphan.long"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
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

				pr, err := rt.ResolveProjectRuntime(ctx, project.ID)
				if err != nil {
					return nil, err
				}
				svc := app.NewOrphanService(rt.store, pr.Snapshot, pr.PreviousSnapshot)
				preview, err := svc.Preview(ctx, project)
				if err != nil {
					return nil, err
				}
				if preview.Total == 0 {
					return map[string]any{"project": project, "report": preview, "applied": false}, nil
				}

				if dryRun || !confirm {
					return nil, domain.NewError(domain.ErrValidation,
						"orphans migration requires --confirm; re-run with --confirm to apply",
						map[string]any{
							"project": project.Slug,
							"report":  preview,
							"hint":    "okt workflow orphans --confirm",
						})
				}

				applied, err := svc.Migrate(ctx, project)
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "report": applied, "applied": true}, nil
			})
		},
	}

	cmd.Flags().BoolVar(&confirm, "confirm", false, opts.t("cli.workflow.orphan.flag.confirm"))
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, opts.t("cli.workflow.orphan.flag.dry-run"))
	return cmd
}
