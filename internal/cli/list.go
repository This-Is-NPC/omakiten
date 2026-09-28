package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/operation"
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

				input := operation.ListTasksInput{
					ProjectSelector: opts.projectSelector(),
					BucketKey:       bucket,
				}
				if cmd.Flags().Changed("parent") {
					// Tri-state via the sentinel `0`: zero requests roots
					// only (parent_id IS NULL) and any positive id scopes
					// the listing to that parent's direct children. The
					// flag stays absent → no filter (every task surfaces).
					if parent == 0 {
						input.ParentID = operation.OptionalInt64{Set: true, Value: nil}
					} else {
						pid := parent
						input.ParentID = operation.OptionalInt64{Set: true, Value: &pid}
					}
				}
				return rt.operationService().ListTasks(ctx, input)
			})
		},
	}

	cmd.Flags().StringVarP(&bucket, "bucket", "b", "", opts.t("cli.task.list.flag.bucket"))
	cmd.Flags().Int64Var(&parent, "parent", 0, opts.t("cli.task.list.flag.parent"))
	return cmd
}
