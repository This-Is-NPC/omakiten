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
		Short: opts.t("cli.config.short"),
	}

	validate := &cobra.Command{
		Use:   "validate [path]",
		Short: opts.t("cli.config.validate.short"),
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
					return nil, domain.NewError(domain.ErrConfigInvalid, t("cli.err.config_invalid"), map[string]any{"path": path, "error": fmt.Sprint(err)})
				}
				return map[string]any{"path": path, "kit": bundle.Kit}, nil
			})
		},
	}

	presets := &cobra.Command{
		Use:   "presets",
		Short: opts.t("cli.config.presets.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(context.Context) (any, error) {
				return map[string]any{"presets": resolvedPresets(opts)}, nil
			})
		},
	}

	cmd.AddCommand(validate)
	cmd.AddCommand(presets)
	cmd.AddCommand(newConfigInitCommand(opts))
	cmd.AddCommand(newConfigShowCommand(opts))
	cmd.AddCommand(newConfigPathCommand(opts))
	cmd.AddCommand(newConfigWhyCommand(opts))
	cmd.AddCommand(newConfigDiffCommand(opts))
	cmd.AddCommand(newConfigLanguageCommand(opts))
	return cmd
}

// resolvedPresets decorates each bundled preset with its catalog-resolved
// title and description. The Preset struct itself stays language-free
// (see task #82 §13) so JSON output is built here at the CLI boundary
// where the catalog is in scope.
func resolvedPresets(opts *runtimeOptions) []map[string]string {
	presets := config.ListPresets()
	out := make([]map[string]string, len(presets))
	for i, p := range presets {
		out[i] = map[string]string{
			"name":        p.Name,
			"title":       opts.t("cli.preset." + p.Name + ".title"),
			"description": opts.t("cli.preset." + p.Name + ".description"),
		}
	}
	return out
}
