package cli

import (
	"context"
	"fmt"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/token"
	"omakiten/internal/tui"
)

func newTUICommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the terminal UI",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTUI(cmd.Context(), opts)
		},
	}
}

func runTUI(ctx context.Context, opts *runtimeOptions) error {
	rt, err := opts.open(ctx, true)
	if err != nil {
		return err
	}
	defer func() { _ = rt.store.Close() }()

	project, err := opts.resolveProject(ctx, rt.store)
	if err != nil {
		return err
	}
	theme, err := loadActiveTheme(rt.configPath)
	if err != nil {
		return err
	}

	editor := app.NewBundleEditor(rt.store, rt.configPath)
	model, err := tui.NewModel(ctx, project, tui.Repositories{
		Tasks:        rt.store,
		Comments:     rt.store,
		Dependencies: rt.store,
		Entries:      rt.store,
		Config:       rt.store,
		Editor:       editor,
	}, theme, token.NewCounter())
	if err != nil {
		return err
	}

	program := tea.NewProgram(model, tea.WithAltScreen())
	_, err = program.Run()
	return err
}

func loadActiveTheme(configPath string) (config.Theme, error) {
	bundle, err := config.LoadBundle(configPath)
	if err != nil {
		return config.Theme{}, domain.NewError(domain.ErrConfigInvalid, "config is invalid", map[string]any{"path": configPath, "error": fmt.Sprint(err)})
	}
	themePath := filepath.Join(filepath.Dir(configPath), "themes", bundle.Config.Theme.Active+".yaml")
	theme, err := config.LoadTheme(themePath)
	if err != nil {
		return config.Theme{}, domain.NewError(domain.ErrConfigInvalid, "theme is invalid", map[string]any{"path": themePath, "error": fmt.Sprint(err)})
	}
	return theme, nil
}
