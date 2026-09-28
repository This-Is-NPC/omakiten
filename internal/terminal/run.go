// Package terminal assembles the interactive adapter from runtime resources.
// CLI command parsing and application services do not import the TUI.
package terminal

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/agentruntime"
	"omakiten/internal/config"
	"omakiten/internal/configstore"
	"omakiten/internal/contract"
	hookactions "omakiten/internal/hooks/actions"
	"omakiten/internal/paths"
	"omakiten/internal/token"
	"omakiten/internal/tui"
)

func Run(ctx context.Context, session agentruntime.Session) error {
	theme := session.Snapshot.Theme()

	bundleStore := configstore.New()
	model, err := tui.NewModel(ctx, session.Project, tui.WireAppServices(tui.Repositories{
		DispatchAction: func(ctx context.Context, request contract.ActionRequest) (contract.ActionResult, error) {
			entry := session.Cache.Get(request.RuntimeID)
			if entry == nil || entry.Service == nil {
				return contract.ActionResult{}, fmt.Errorf("action service is unavailable")
			}
			return entry.Service.ForTUI().ExecuteAction(ctx, request)
		},
		ResolveCommandPreview: agentruntime.ResolveCommandPreview,
		DeleteProject:         agentruntime.ProjectDeleter(session.Store, session.Store, session.DBPath, session.Snapshot.Settings().Backup.RetentionCount),
		Tasks:                 session.Store,
		Projects:              session.Store,
		Comments:              session.Store,
		Dependencies:          session.Store,
		Tags:                  session.Store,
		BundleStore:           bundleStore,
		ActivityLogs:          session.Store,
		Events:                session.Store,
		Orphans:               session.Store,
		Plans:                 session.Store,
		Watermark:             session.Store,
		ConfigPath:            session.ConfigPath,
		DBPath:                session.DBPath,
		Version:               session.Version,
		RepoLocalDir:          session.RepoLocalDir,
		Cache:                 session.Cache,
		ProjectID:             session.CacheProjectID,
		Catalog:               session.Snapshot.Catalog(config.SurfaceTUI),
	}, session.ConfigPath), theme, token.NewCounter(), session.Snapshot.Settings().TUI.TokenBadge, session.Snapshot.Priorities(), session.Snapshot.Severities(), tui.NotificationBinding{
		Notifications: session.Snapshot.Notifications(),
	})
	if err != nil {
		return err
	}

	program := tea.NewProgram(model, tea.WithAltScreen())
	if entry := session.Cache.Get(session.CacheProjectID); entry != nil && entry.NotificationAction != nil {
		entry.NotificationAction.SetSender(teaNotificationSender{program: program})
	}
	finalModel, runErr := program.Run()
	// The shell wrapper installed by install.sh / install.ps1 reads the
	// path written here and `cd`s the parent shell after the TUI exits.
	// Without the wrapper this is a silent no-op; the TUI itself never
	// changes the parent shell's CWD (it cannot).
	if final, ok := finalModel.(tui.Model); ok {
		if root := final.LastProjectRoot(); root != "" {
			_ = paths.WriteCDPath(root)
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
