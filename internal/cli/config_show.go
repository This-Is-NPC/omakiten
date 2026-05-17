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
	"omakiten/internal/sqlite"
)

func newConfigShowCommand(opts *runtimeOptions) *cobra.Command {
	var scope string
	cmd := &cobra.Command{
		Use:   "show",
		Short: opts.t("cli.config.show.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				if err := primeDiscoveryStart(ctx, opts); err != nil {
					return nil, err
				}
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
	cmd.Flags().StringVar(&scope, "scope", "", opts.t("cli.config.show.flag.scope"))
	_ = cmd.MarkFlagRequired("scope")
	return cmd
}

func newConfigPathCommand(opts *runtimeOptions) *cobra.Command {
	var scope string
	cmd := &cobra.Command{
		Use:   "path",
		Short: opts.t("cli.config.why.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				if err := primeDiscoveryStart(ctx, opts); err != nil {
					return nil, err
				}
				root, err := resolveInstallRootForScope(opts, scope)
				if err != nil {
					return nil, err
				}
				return map[string]any{"scope": scope, "path": root}, nil
			})
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "", opts.t("cli.config.why.flag.scope"))
	_ = cmd.MarkFlagRequired("scope")
	return cmd
}

// primeDiscoveryStart populates opts.discoveryStart so walk-up honours
// --project / --project-id when supplied. Subcommands that do not need a
// full runtime open the DB just long enough to do the project lookup,
// then close it. Failures fall back to CWD silently so the subcommand can
// proceed with the user-global resolver when the DB is fresh or the
// project isn't registered yet.
func primeDiscoveryStart(ctx context.Context, opts *runtimeOptions) error {
	if opts.discoveryStart != "" {
		return nil
	}
	if opts.project == "" && opts.projectID == 0 {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		opts.discoveryStart = cwd
		return nil
	}
	dbPath, err := opts.resolvedDBPath()
	if err != nil {
		return err
	}
	store, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		// DB not openable yet — fall back to CWD.
		cwd, _ := os.Getwd()
		opts.discoveryStart = cwd
		return nil
	}
	defer func() { _ = store.Close() }()
	start, err := opts.resolveDiscoveryStart(ctx, store)
	if err != nil {
		return err
	}
	opts.discoveryStart = start
	return nil
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
		start := opts.discoveryStart
		if start == "" {
			cwd, err := os.Getwd()
			if err != nil {
				return "", err
			}
			start = cwd
		}
		dir, ok, err := config.FindRepoLocal(start)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", domain.NewError(domain.ErrValidation, "no repo-local .omakiten/ found above start dir", map[string]any{"start": start})
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
