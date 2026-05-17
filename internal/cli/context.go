package cli

import (
	"context"

	"github.com/spf13/cobra"
)

func newContextCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: opts.t("cli.context.short"),
	}

	var body string
	add := &cobra.Command{
		Use:   "add",
		Short: opts.t("cli.context.add.short"),
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

				entry, err := rt.contextService().Add(ctx, project, body)
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "context_entry": entry}, nil
			})
		},
	}
	add.Flags().StringVarP(&body, "body", "b", "", opts.t("cli.context.add.flag.body"))
	_ = add.MarkFlagRequired("body")

	var level int
	dump := &cobra.Command{
		Use:   "dump",
		Short: opts.t("cli.context.dump.short"),
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

				return rt.contextService().Dump(ctx, project, level)
			})
		},
	}
	dump.Flags().IntVarP(&level, "level", "l", 2, opts.t("cli.context.dump.flag.level"))

	cmd.AddCommand(add)
	cmd.AddCommand(dump)
	return cmd
}
