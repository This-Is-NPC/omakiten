package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/app"
	"omakiten/internal/domain"
)

func newListCommand(opts *runtimeOptions) *cobra.Command {
	var bucket string
	var parent int64

	cmd := &cobra.Command{
		Use:   "list",
		Short: opts.t("cli.task.list.short"),
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

				filter := domain.TaskFilter{BucketKey: bucket}
				if cmd.Flags().Changed("parent") {
					// Tri-state via the sentinel `0`: zero requests roots
					// only (parent_id IS NULL) and any positive id scopes
					// the listing to that parent's direct children. The
					// flag stays absent → no filter (every task surfaces).
					if parent == 0 {
						filter.ParentMode = domain.ParentRoots
					} else {
						filter.ParentMode = domain.ParentChildren
						filter.ParentValue = parent
					}
				}

				tasks, err := app.NewTaskServiceFromStore(rt.store, rt.activeRegistry(), rt.activeSnapshot()).List(ctx, project, filter)
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "tasks": tasks}, nil
			})
		},
	}

	cmd.Flags().StringVarP(&bucket, "bucket", "b", "", opts.t("cli.task.list.flag.bucket"))
	cmd.Flags().Int64Var(&parent, "parent", 0, opts.t("cli.task.list.flag.parent"))
	return cmd
}
