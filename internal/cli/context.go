package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/app"
	"omakiten/internal/token"
)

func newContextCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Inspect project context for handoff",
	}

	var body string
	add := &cobra.Command{
		Use:   "add",
		Short: "Add a project handoff context entry",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer func() { _ = rt.store.Close() }()
				ctx = rt.WithActivityRepo(ctx)

				project, err := opts.resolveProject(ctx, rt.store)
				if err != nil {
					return nil, err
				}

				entry, err := app.NewContextService(rt.store, rt.store, rt.store, rt.store, rt.store, token.NewCounter()).Add(ctx, project, body)
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "context_entry": entry}, nil
			})
		},
	}
	add.Flags().StringVarP(&body, "body", "b", "", "context body")
	_ = add.MarkFlagRequired("body")

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
				ctx = rt.WithActivityRepo(ctx)

				project, err := opts.resolveProject(ctx, rt.store)
				if err != nil {
					return nil, err
				}

				return app.NewContextService(rt.store, rt.store, rt.store, rt.store, rt.store, token.NewCounter()).Dump(ctx, project, level)
			})
		},
	}
	dump.Flags().IntVarP(&level, "level", "l", 2, "context detail level: 1, 2, or 3")

	cmd.AddCommand(add)
	cmd.AddCommand(dump)
	return cmd
}
