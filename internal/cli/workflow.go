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
		Short: "Inspect workflow configuration",
	}

	show := &cobra.Command{
		Use:   "show",
		Short: "Show the active workflow",
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
				return map[string]any{"workflow": rt.store.Snapshot().Workflow()}, nil
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
		Short: "Preview or rebind tasks whose bucket was deactivated by a workflow swap",
		Long: `Tasks that pointed to a workflow bucket which no longer exists after a
preset switch or omakiten.yaml edit are orphans. By default this command
prints the migration plan (which tasks would rebind to which bucket) and
exits non-zero so nothing is mutated.

Pass --confirm to apply the rebind. Each migrated task emits a
task.migrated event with from/to/reason payload. --dry-run is equivalent
to running without --confirm.`,
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

				svc := app.NewOrphanService(rt.store)
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

	cmd.Flags().BoolVar(&confirm, "confirm", false, "apply the rebind; without this flag the command prints the plan and exits")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the migration plan without applying (default when --confirm is absent)")
	return cmd
}
