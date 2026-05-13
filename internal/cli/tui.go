package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"omakiten/internal/activity"
	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/configstore"
	"omakiten/internal/domain"
	hookactions "omakiten/internal/hooks/actions"
	"omakiten/internal/token"
	"omakiten/internal/tui"
)

func newTUICommand(opts *runtimeOptions, version string) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the terminal UI",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTUI(cmd.Context(), opts, version)
		},
	}
}

func runTUI(ctx context.Context, opts *runtimeOptions, version string) error {
	rt, err := opts.open(ctx, true)
	if err != nil {
		return err
	}
	defer rt.close()

	ctx = activity.WithAgent(ctx, "tui", "tui", "human", "")
	ctx = rt.WithActivityRepo(ctx)

	project, err := opts.resolveProject(ctx, rt.store)
	if err != nil {
		// Without an explicit --project / --project-id, an unresolvable CWD
		// is not an error — it is the trigger for the multi-project Home
		// screen. Explicit flags must still 404 loudly so typos are caught.
		if opts.projectID == 0 && opts.project == "" && isProjectNotFoundError(err) {
			project = domain.ProjectContext{}
		} else {
			return err
		}
	}
	bundle, err := config.LoadBundle(rt.configPath)
	if err != nil {
		return domain.NewError(domain.ErrConfigInvalid, "config is invalid", map[string]any{"path": rt.configPath, "error": fmt.Sprint(err)})
	}
	theme, err := loadActiveThemeFromBundle(bundle, rt.configPath)
	if err != nil {
		return err
	}

	bundleStore := configstore.New()
	editor := app.NewBundleEditor(rt.store, bundleStore, rt.configPath)
	model, err := tui.NewModel(ctx, project, tui.Repositories{
		Tasks:        rt.store,
		Projects:     rt.store,
		Workflow:     app.NewWorkflowServiceFromStore(rt.store, rt.registry),
		Comments:     rt.store,
		Dependencies: rt.store,
		Entries:      rt.store,
		Config:       rt.store,
		Tags:         rt.store,
		Editor:       editor,
		BundleStore:  bundleStore,
		EntityFiles:  bundleStore,
		Slugger:      bundleStore,
		ActivityLogs: rt.store,
		Events:       rt.store,
		Metrics:      app.NewMetricsService(rt.store),
		Orphans:      rt.store,
		ConfigPath:   rt.configPath,
		DBPath:       rt.dbPath,
		Version:      version,
	}, theme, token.NewCounter(), bundle.Config.TUI.TokenBadge, bundle.Config.EffectivePriorities(), bundle.Config.EffectiveSeverities(), tui.NotificationBinding{
		Notifications: bundle.Notifications,
	})
	if err != nil {
		return err
	}

	program := tea.NewProgram(model, tea.WithAltScreen())
	if rt.notificationAction != nil {
		rt.notificationAction.SetSender(teaNotificationSender{program: program})
	}
	finalModel, runErr := program.Run()
	// The shell wrapper installed by install.sh / install.ps1 reads the
	// path written here and `cd`s the parent shell after the TUI exits.
	// Without the wrapper this is a silent no-op; the TUI itself never
	// changes the parent shell's CWD (it cannot).
	if final, ok := finalModel.(tui.Model); ok {
		if root := final.LastProjectRoot(); root != "" {
			_ = writeOktCDPath(root)
		}
	}
	return runErr
}

type teaNotificationSender struct {
	program *tea.Program
}

func (s teaNotificationSender) SendNotification(msg hookactions.NotificationShowMsg) {
	s.program.Send(msg)
}

// isProjectNotFoundError returns true when the resolver signalled that the
// current working directory is not inside any registered project. We unwrap
// the domain CodedError to compare codes rather than match on message text.
func isProjectNotFoundError(err error) bool {
	var coded *domain.CodedError
	if errors.As(err, &coded) {
		return coded.Code == domain.ErrProjectNotFound
	}
	return false
}

// writeOktCDPath writes the absolute project root path to the channel the
// shell wrapper reads after the TUI exits. Resolution order, mirroring the
// wrapper itself: $OKT_CD_FILE → $XDG_RUNTIME_DIR/okt-cd → $TMPDIR/okt-cd-$UID
// → /tmp/okt-cd-$UID. Best-effort: an I/O failure here is not surfaced to
// the user because the wrapper treats a missing file as "no cd needed".
func writeOktCDPath(root string) error {
	target := oktCDPath()
	if target == "" {
		return nil
	}
	return os.WriteFile(target, []byte(root+"\n"), 0o600)
}

func oktCDPath() string {
	if path := os.Getenv("OKT_CD_FILE"); path != "" {
		return path
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "okt-cd")
	}
	tmp := os.Getenv("TMPDIR")
	if tmp == "" {
		tmp = "/tmp"
	}
	return filepath.Join(tmp, "okt-cd-"+strconv.Itoa(os.Getuid()))
}

func loadActiveThemeFromBundle(bundle config.Bundle, configPath string) (config.Theme, error) {
	root := config.ConfigRootFromYAMLPath(configPath)
	active := bundle.Config.Theme.Active
	// Resolution order: <root>/themes/custom/<slug>.yaml (user override) →
	// <root>/themes/<slug>.yaml (default). The custom path lets users tweak the
	// shipped theme or add new ones without losing them on default refresh.
	customPath := filepath.Join(root, "themes", "custom", active+".yaml")
	defaultPath := filepath.Join(root, "themes", active+".yaml")
	themePath := defaultPath
	if _, err := os.Stat(customPath); err == nil {
		themePath = customPath
	}
	theme, err := config.LoadTheme(themePath)
	if err != nil {
		return config.Theme{}, domain.NewError(domain.ErrConfigInvalid, "theme is invalid", map[string]any{"path": themePath, "error": fmt.Sprint(err)})
	}
	return theme, nil
}
