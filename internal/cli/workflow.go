package cli

import (
	"context"

	"github.com/spf13/cobra"
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
				ctx = rt.WithActivityRepo(ctx)

				workflow, err := rt.store.ActiveWorkflow(ctx)
				if err != nil {
					return nil, err
				}
				return map[string]any{"workflow": workflow}, nil
			})
		},
	}

	cmd.AddCommand(show)
	return cmd
}
