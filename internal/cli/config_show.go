package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/paths"
)

func newConfigShowCommand(opts *runtimeOptions) *cobra.Command {
	var scope string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Print the raw active yaml for the chosen scope",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(context.Context) (any, error) {
				path, err := resolveActiveFileForScope(opts, scope)
				if err != nil {
					return nil, err
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return nil, domain.NewError(domain.ErrValidation, "config file not readable", map[string]any{"path": path, "error": err.Error()})
				}
				return map[string]any{"scope": scope, "path": path, "content": string(data)}, nil
			})
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "", "global or local")
	_ = cmd.MarkFlagRequired("scope")
	return cmd
}

func newConfigPathCommand(opts *runtimeOptions) *cobra.Command {
	var scope string
	cmd := &cobra.Command{
		Use:   "path",
		Short: "Print the install root that owns the chosen scope's config layer",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(context.Context) (any, error) {
				root, err := resolveInstallRootForScope(opts, scope)
				if err != nil {
					return nil, err
				}
				return map[string]any{"scope": scope, "path": root}, nil
			})
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "", "global or local")
	_ = cmd.MarkFlagRequired("scope")
	return cmd
}

// resolveInstallRootForScope returns the directory that owns the chosen
// scope's install. Global honours --config when supplied; local walks up
// from CWD looking for .omakiten/ (mirrors runtime discovery). Missing
// local discovery is a validation_error rather than a silent fallback.
func resolveInstallRootForScope(opts *runtimeOptions, scope string) (string, error) {
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
		dir, ok, err := config.FindRepoLocal(cwd)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", domain.NewError(domain.ErrValidation, "no repo-local .omakiten/ found above CWD", map[string]any{"cwd": cwd})
		}
		return dir, nil
	default:
		return "", domain.NewError(domain.ErrValidation, "invalid --scope (want global or local)", map[string]any{"scope": scope})
	}
}

// resolveActiveFileForScope returns the path of the active yaml file for the
// chosen scope. Reuses resolveInstallRootForScope and then applies the
// shared .active discipline via paths.ActiveConfigFileInDir.
func resolveActiveFileForScope(opts *runtimeOptions, scope string) (string, error) {
	root, err := resolveInstallRootForScope(opts, scope)
	if err != nil {
		return "", err
	}
	switch scope {
	case "global":
		if opts.configPath != "" {
			return filepath.Abs(opts.configPath)
		}
		return paths.ActiveConfigFileInDir(filepath.Join(root, "config"))
	case "local":
		return paths.ActiveConfigFileInDir(filepath.Join(root, "config"))
	default:
		return "", fmt.Errorf("unreachable scope %q", scope)
	}
}
