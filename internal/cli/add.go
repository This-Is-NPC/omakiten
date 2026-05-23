package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/app"
	"omakiten/internal/domain"
)

func newAddCommand(opts *runtimeOptions) *cobra.Command {
	var title string
	var description string
	var bucket string
	var parent int64

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

				service := app.NewTaskServiceFromStore(rt.store, rt.activeRegistry(), rt.activeSnapshot())
				var task domain.Task
				if cmd.Flags().Changed("parent") {
					task, err = service.AddSub(ctx, project, parent, title, description, "", bucket)
				} else {
					task, err = service.Add(ctx, project, title, description, "", bucket)
				}
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
	cmd.Flags().Int64Var(&parent, "parent", 0, opts.t("cli.task.add.flag.parent"))
	return cmd
}
