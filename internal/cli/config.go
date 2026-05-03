package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/config"
)

func newConfigCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage the canonical omakiten.yaml bundle",
	}

	validate := &cobra.Command{
		Use:   "validate [path]",
		Short: "Validate an omakiten.yaml file",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(context.Context) (any, error) {
				path := ""
				if len(args) > 0 {
					path = args[0]
				} else {
					var err error
					path, err = opts.resolvedConfigPath()
					if err != nil {
						return nil, err
					}
				}

				bundle, err := config.LoadBundle(path)
				if err != nil {
					return nil, err
				}
				return map[string]any{"path": path, "kit": bundle.Kit}, nil
			})
		},
	}

	cmd.AddCommand(validate)
	return cmd
}
