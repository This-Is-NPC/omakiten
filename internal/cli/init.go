package cli

import (
	"context"
	"os"

	"github.com/spf13/cobra"

	"omakiten/internal/app"
)

func newInitCommand(opts *runtimeOptions) *cobra.Command {
	var name string
	var slug string
	var root string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Register the current project in the global database",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer func() { _ = rt.store.Close() }()

				projectRoot := root
				if projectRoot == "" {
					projectRoot, err = os.Getwd()
					if err != nil {
						return nil, err
					}
				}

				project, err := app.NewProjectService(rt.store).Init(ctx, name, slug, projectRoot)
				if err != nil {
					return nil, err
				}

				return map[string]any{"project": project, "db_path": rt.dbPath, "config_path": rt.configPath}, nil
			})
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "project name")
	cmd.Flags().StringVar(&slug, "slug", "", "project slug")
	cmd.Flags().StringVar(&root, "root", "", "project root path")
	return cmd
}
