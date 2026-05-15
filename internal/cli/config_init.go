package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

func newConfigInitCommand(opts *runtimeOptions) *cobra.Command {
	var scopeFlag string
	var presetName string
	var force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Seed a workflow preset into the user-global or repo-local config layer",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(context.Context) (any, error) {
				scope, err := parseScope(scopeFlag)
				if err != nil {
					return nil, err
				}
				seedOpts, root, err := resolveSeedOptions(opts, scope)
				if err != nil {
					return nil, err
				}

				res, err := config.SeedPreset(scope, presetName, force, seedOpts)
				if errors.Is(err, config.ErrPresetTargetExists) && !force {
					confirmed, perr := confirmOverwrite(cmd.InOrStdin(), cmd.ErrOrStderr(), targetPath(scope, seedOpts, presetName))
					if perr != nil {
						return nil, perr
					}
					if !confirmed {
						return nil, presetCLIError(err)
					}
					res, err = config.SeedPreset(scope, presetName, true, seedOpts)
				}
				if err != nil {
					return nil, presetCLIError(err)
				}

				payload := map[string]any{
					"scope": string(scope),
					"root":  root,
					"preset": map[string]any{
						"name":  res.Preset.Name,
						"title": res.Preset.Title,
						"path":  res.Path,
					},
				}
				if res.NoOp {
					payload["no_op"] = true
				}
				if res.Overwritten {
					payload["overwritten"] = true
				}
				return payload, nil
			})
		},
	}

	cmd.Flags().StringVar(&scopeFlag, "scope", "", "config layer to seed: global or local")
	cmd.Flags().StringVar(&presetName, "preset", "", "official workflow preset to seed")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing config file even when its contents diverge from the preset")
	_ = cmd.MarkFlagRequired("scope")
	_ = cmd.MarkFlagRequired("preset")
	return cmd
}

func parseScope(raw string) (config.Scope, error) {
	switch raw {
	case "global":
		return config.ScopeGlobal, nil
	case "local":
		return config.ScopeLocal, nil
	default:
		return "", domain.NewError(domain.ErrValidation, "invalid --scope (want global or local)", map[string]any{"scope": raw})
	}
}

// resolveSeedOptions resolves the SeedOptions and a human-readable "root"
// label for the chosen scope. Global scope honours --config (via
// resolvedConfigRoot); local scope writes to the literal CWD without
// walk-up so callers in monorepos place the overlay exactly where they
// invoked the command.
func resolveSeedOptions(opts *runtimeOptions, scope config.Scope) (config.SeedOptions, string, error) {
	switch scope {
	case config.ScopeGlobal:
		root, err := opts.resolvedConfigRoot()
		if err != nil {
			return config.SeedOptions{}, "", err
		}
		return config.SeedOptions{GlobalRoot: root}, root, nil
	case config.ScopeLocal:
		cwd, err := os.Getwd()
		if err != nil {
			return config.SeedOptions{}, "", err
		}
		return config.SeedOptions{LocalRoot: cwd}, cwd, nil
	default:
		return config.SeedOptions{}, "", fmt.Errorf("resolve seed options: unknown scope %q", scope)
	}
}

func targetPath(scope config.Scope, opts config.SeedOptions, name string) string {
	switch scope {
	case config.ScopeGlobal:
		return filepath.Join(opts.GlobalRoot, "config", name+".yaml")
	case config.ScopeLocal:
		return filepath.Join(opts.LocalRoot, config.RepoLocalDirName, "config", name+".yaml")
	default:
		return ""
	}
}
