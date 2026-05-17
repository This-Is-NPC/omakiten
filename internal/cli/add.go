package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/app"
)

func newAddCommand(opts *runtimeOptions) *cobra.Command {
	var title string
	var description string
	var bucket string

	cmd := &cobra.Command{
		Use:   "add",
		Short: opts.t("cli.task.add.short"),
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

				task, err := app.NewTaskServiceFromStore(rt.store, rt.activeRegistry(), rt.activeSnapshot()).Add(ctx, project, title, description, "", bucket)
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "task": task}, nil
			})
		},
	}

	cmd.Flags().StringVarP(&title, "title", "t", "", opts.t("cli.task.add.flag.title"))
	cmd.Flags().StringVarP(&description, "description", "d", "", opts.t("cli.task.add.flag.description"))
	cmd.Flags().StringVarP(&bucket, "bucket", "b", "", opts.t("cli.task.add.flag.bucket"))
	return cmd
}
