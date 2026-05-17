package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/app"
	"omakiten/internal/domain"
)

func newListCommand(opts *runtimeOptions) *cobra.Command {
	var bucket string

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

				tasks, err := app.NewTaskServiceFromStore(rt.store, rt.activeRegistry(), rt.activeSnapshot()).List(ctx, project, domain.TaskFilter{BucketKey: bucket})
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "tasks": tasks}, nil
			})
		},
	}

	cmd.Flags().StringVarP(&bucket, "bucket", "b", "", opts.t("cli.task.list.flag.bucket"))
	return cmd
}
