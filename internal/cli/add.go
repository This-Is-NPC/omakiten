package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/operation"
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

				input := operation.CreateTaskInput{
					ProjectSelector:     opts.projectSelector(),
					Title:               title,
					Description:         description,
					BucketKey:           bucket,
					SkipSimilarityCheck: true,
				}
				if cmd.Flags().Changed("parent") {
					input.ParentID = &parent
				}
				return rt.operationService().CreateTask(ctx, input)
			})
		},
	}

	cmd.Flags().StringVarP(&title, "title", "t", "", opts.t("cli.task.add.flag.title"))
	cmd.Flags().StringVarP(&description, "description", "d", "", opts.t("cli.task.add.flag.description"))
	cmd.Flags().StringVarP(&bucket, "bucket", "b", "", opts.t("cli.task.add.flag.bucket"))
	cmd.Flags().Int64Var(&parent, "parent", 0, opts.t("cli.task.add.flag.parent"))
	return cmd
}
