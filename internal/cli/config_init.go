package cli

import (
	"context"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/paths"
)

func newConfigInitCommand(opts *runtimeOptions) *cobra.Command {
	var scopeFlag string
	var presetName string
	var force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Materialise an official preset into the user-global or repo-local install",
		Long: `Seed a standalone config install rooted in the chosen scope.

  --scope global  →  paths.ConfigRoot() (or --config's parent).
                     Existing user install; rerun is idempotent.

  --scope local   →  <cwd>/.omakiten (literal CWD, no walk-up).
                     A complete install — config/<preset>.yaml plus every
                     entity folder — so the runtime can load it without any
                     merge with the user-global layer. The walk-up resolver
                     picks this up automatically on subsequent okt calls
                     from inside the repo.

Behaviour matrix:
  - File missing            → atomic write.
  - Same preset, same files → no_op:true (silent success).
  - Different preset        → flips .active to the new preset.
  - Tampered shipped files  → preserved unless --force.
  - --force                 → re-copies every embedded shipped file
                              (skills, laws, personas, templates, themes,
                              notifications, every preset yaml).
                              custom/ subtrees are never touched.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(context.Context) (any, error) {
				root, err := resolveScopeRoot(opts, scopeFlag)
				if err != nil {
					return nil, err
				}
				res, err := config.SeedInstall(root, presetName, force)
				if err != nil {
					return nil, presetCLIError(err)
				}
				payload := map[string]any{
					"scope": scopeFlag,
					"root":  root,
					"preset": map[string]any{
						"name": res.PresetName,
						"path": res.Path,
					},
				}
				if res.NoOp {
					payload["no_op"] = true
				}
				if res.Refreshed {
					payload["refreshed"] = true
				}
				return payload, nil
			})
		},
	}
	cmd.Flags().StringVar(&scopeFlag, "scope", "", "config layer to seed: global or local")
	cmd.Flags().StringVar(&presetName, "preset", "", "official workflow preset to seed")
	cmd.Flags().BoolVar(&force, "force", false, "re-copy embedded shipped files (skills, laws, personas, etc.) — custom/ is never touched")
	_ = cmd.MarkFlagRequired("scope")
	_ = cmd.MarkFlagRequired("preset")
	return cmd
}

// resolveScopeRoot returns the directory SeedInstall should populate for the
// chosen scope. Global honours --config (deriving the ConfigRoot via
// ConfigRootFromYAMLPath) and otherwise falls back to paths.ConfigRoot();
// local writes to <cwd>/.omakiten literally without walk-up so monorepos
// place the install exactly where the user invoked the command.
func resolveScopeRoot(opts *runtimeOptions, scope string) (string, error) {
	switch scope {
	case "global":
		if opts.configPath != "" {
			abs, err := filepath.Abs(opts.configPath)
			if err != nil {
				return "", err
			}
			return config.ConfigRootFromYAMLPath(abs), nil
		}
		return paths.ConfigRoot()
	case "local":
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return filepath.Join(cwd, config.RepoLocalDirName), nil
	default:
		return "", domain.NewError(domain.ErrValidation, "invalid --scope (want global or local)", map[string]any{"scope": scope})
	}
}

