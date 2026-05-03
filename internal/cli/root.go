package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/output"
	"omakiten/internal/paths"
	projectresolver "omakiten/internal/project"
	"omakiten/internal/sqlite"
)

type runtimeOptions struct {
	dbPath     string
	configPath string
	project    string
	projectID  int64
}

type runtime struct {
	store      *sqlite.Store
	configPath string
	dbPath     string
}

func NewRootCommand(version string) *cobra.Command {
	opts := &runtimeOptions{}
	cmd := &cobra.Command{
		Use:           "okt",
		Short:         "Opinionated checkpoints for AI-driven development",
		Long: `okt drives Omakiten from the command line and TUI.

Path resolution (highest to lowest precedence):
  1. --config / --db flags
  2. $OMAKITEN_HOME — pins config to <HOME>/config/omakiten.yaml and data to <HOME>/data/omakiten.db
  3. $XDG_CONFIG_HOME / $XDG_DATA_HOME
  4. ~/.config/omakiten and ~/.local/share/omakiten`,
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.PersistentFlags().StringVar(&opts.dbPath, "db", "", "SQLite database path (overrides $OMAKITEN_HOME and XDG)")
	cmd.PersistentFlags().StringVar(&opts.configPath, "config", "", "omakiten.yaml path (overrides $OMAKITEN_HOME and XDG)")
	cmd.PersistentFlags().StringVarP(&opts.project, "project", "p", "", "project slug")
	cmd.PersistentFlags().Int64Var(&opts.projectID, "project-id", 0, "project id")

	cmd.AddCommand(newInitCommand(opts))
	cmd.AddCommand(newAddCommand(opts))
	cmd.AddCommand(newListCommand(opts))
	cmd.AddCommand(newMoveCommand(opts))
	cmd.AddCommand(newEditCommand(opts))
	cmd.AddCommand(newCommentCommand(opts))
	cmd.AddCommand(newDependCommand(opts))
	cmd.AddCommand(newContextCommand(opts))
	cmd.AddCommand(newWorkflowCommand(opts))
	cmd.AddCommand(newConfigCommand(opts))
	cmd.AddCommand(newLawCommand(opts))
	cmd.AddCommand(newSkillCommand(opts))
	cmd.AddCommand(newPersonaCommand(opts))
	cmd.AddCommand(newTUICommand(opts))

	return cmd
}

func (o *runtimeOptions) open(ctx context.Context, materializeConfig bool) (*runtime, error) {
	configPath, err := o.resolvedConfigPath()
	if err != nil {
		return nil, err
	}
	dbPath, err := o.resolvedDBPath()
	if err != nil {
		return nil, err
	}

	if materializeConfig {
		if err := config.EnsureDefaultFiles(filepath.Dir(configPath)); err != nil {
			return nil, err
		}
	}

	store, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		return nil, err
	}

	if materializeConfig {
		if _, _, err := app.NewConfigService(store).Import(ctx, configPath); err != nil {
			_ = store.Close()
			return nil, err
		}
	}

	return &runtime{store: store, configPath: configPath, dbPath: dbPath}, nil
}

func (o *runtimeOptions) resolvedConfigPath() (string, error) {
	if o.configPath != "" {
		return filepath.Abs(o.configPath)
	}
	return paths.ConfigFile()
}

func (o *runtimeOptions) resolvedDBPath() (string, error) {
	if o.dbPath != "" {
		return filepath.Abs(o.dbPath)
	}
	return paths.DatabaseFile()
}

func (o *runtimeOptions) resolveProject(ctx context.Context, store app.ProjectRepository) (domain.ProjectContext, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return domain.ProjectContext{}, err
	}

	resolver := projectresolver.NewResolver(store)
	return resolver.Resolve(ctx, projectresolver.ResolveOptions{ProjectID: o.projectID, Project: o.project, CWD: cwd})
}

func writeSuccess(cmd *cobra.Command, data any) error {
	return output.Write(cmd.OutOrStdout(), output.Success(data))
}

func writeError(cmd *cobra.Command, err error) error {
	var coded *domain.CodedError
	if errors.As(err, &coded) {
		_ = output.Write(cmd.OutOrStdout(), output.Failure(string(coded.Code), coded.Message, coded.Details))
		return exitError{code: 1}
	}

	_ = output.Write(cmd.OutOrStdout(), output.Failure("internal_error", err.Error(), nil))
	return exitError{code: 1}
}

func runJSON(cmd *cobra.Command, fn func(context.Context) (any, error)) error {
	data, err := fn(cmd.Context())
	if err != nil {
		return writeError(cmd, err)
	}
	if err := writeSuccess(cmd, data); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}
