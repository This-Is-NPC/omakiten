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
	var scopeFlag string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Print the raw active omakiten.yaml for the chosen scope",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(context.Context) (any, error) {
				scope, err := parseScope(scopeFlag)
				if err != nil {
					return nil, err
				}
				path, err := resolveScopeActiveFile(opts, scope)
				if err != nil {
					return nil, err
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return nil, domain.NewError(domain.ErrValidation, "config file not readable", map[string]any{"path": path, "error": err.Error()})
				}
				return map[string]any{"scope": string(scope), "path": path, "content": string(data)}, nil
			})
		},
	}
	cmd.Flags().StringVar(&scopeFlag, "scope", "", "global or local")
	_ = cmd.MarkFlagRequired("scope")
	return cmd
}

func newConfigPathCommand(opts *runtimeOptions) *cobra.Command {
	var scopeFlag string
	cmd := &cobra.Command{
		Use:   "path",
		Short: "Print the directory that holds the chosen scope's config layer",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(context.Context) (any, error) {
				scope, err := parseScope(scopeFlag)
				if err != nil {
					return nil, err
				}
				dir, err := resolveScopeRoot(opts, scope)
				if err != nil {
					return nil, err
				}
				return map[string]any{"scope": string(scope), "path": dir}, nil
			})
		},
	}
	cmd.Flags().StringVar(&scopeFlag, "scope", "", "global or local")
	_ = cmd.MarkFlagRequired("scope")
	return cmd
}

// resolveScopeRoot returns the directory that owns the chosen scope's config
// layer. For global this is the user-global ConfigDir (the parent of the
// active .yaml). For local this is the .omakiten/ directory discovered via
// walk-up from CWD; missing discovery yields a not_found error rather than a
// silent fallback to global.
func resolveScopeRoot(opts *runtimeOptions, scope config.Scope) (string, error) {
	switch scope {
	case config.ScopeGlobal:
		if opts.configPath != "" {
			abs, err := filepath.Abs(opts.configPath)
			if err != nil {
				return "", err
			}
			return filepath.Dir(abs), nil
		}
		return paths.ConfigDir()
	case config.ScopeLocal:
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
		return "", fmt.Errorf("resolve scope root: unknown scope %q", scope)
	}
}

// resolveScopeActiveFile returns the active yaml file for the chosen scope.
// For global it defers to opts.resolvedConfigPath (honours --config). For
// local it walks up to .omakiten/ and resolves the active library entry
// inside .omakiten/config/ via the standard .active discipline.
func resolveScopeActiveFile(opts *runtimeOptions, scope config.Scope) (string, error) {
	switch scope {
	case config.ScopeGlobal:
		return opts.resolvedConfigPath()
	case config.ScopeLocal:
		dir, err := resolveScopeRoot(opts, scope)
		if err != nil {
			return "", err
		}
		configDir := filepath.Join(dir, "config")
		path, err := paths.ActiveConfigFileInDir(configDir)
		if err != nil {
			return "", domain.NewError(domain.ErrValidation, "no active library yaml inside repo-local .omakiten/config", map[string]any{"dir": configDir, "error": err.Error()})
		}
		return path, nil
	default:
		return "", fmt.Errorf("resolve scope active file: unknown scope %q", scope)
	}
}
