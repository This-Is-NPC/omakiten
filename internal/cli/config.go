package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"omakiten/internal/config"
	"omakiten/internal/domain"
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
					return nil, domain.NewError(domain.ErrConfigInvalid, "config is invalid", map[string]any{"path": path, "error": fmt.Sprint(err)})
				}
				return map[string]any{"path": path, "kit": bundle.Kit}, nil
			})
		},
	}

	presets := &cobra.Command{
		Use:   "presets",
		Short: "List official workflow presets",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(context.Context) (any, error) {
				return map[string]any{"presets": config.ListPresets()}, nil
			})
		},
	}

	cmd.AddCommand(validate)
	cmd.AddCommand(presets)
	cmd.AddCommand(newConfigInitCommand(opts))
	return cmd
}
