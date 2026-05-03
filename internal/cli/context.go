package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/app"
)

func newContextCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Inspect project context for handoff",
	}

	var level int
	dump := &cobra.Command{
		Use:   "dump",
		Short: "Dump progressive context for agents",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer func() { _ = rt.store.Close() }()

				project, err := opts.resolveProject(ctx, rt.store)
				if err != nil {
					return nil, err
				}

				return app.NewContextService(rt.store, rt.store).Dump(ctx, project, level)
			})
		},
	}
	dump.Flags().IntVar(&level, "level", 2, "context detail level: 1, 2, or 3")

	cmd.AddCommand(dump)
	return cmd
}
