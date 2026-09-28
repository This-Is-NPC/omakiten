package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/agentruntime"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/operation"
	"omakiten/internal/paths"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/board"
	"omakiten/internal/tui/screens/graph"
	"omakiten/internal/tui/screens/home"
	"omakiten/internal/tui/screens/studio"
	"omakiten/internal/tui/screens/table"
)

type homeReloadResultMsg struct {
	generation        uint64
	runtimeGeneration uint64
	payload           home.Payload
	err               error
}

func (m *Model) reloadHome() error {
	payload, err := loadHomePayload(m.ctx, m.repos)
	m.homeScreen = m.homeScreen.Apply(home.Result{Generation: m.homeScreen.Generation(), Payload: payload, Err: err})
	return err
}

func (m Model) homeReloadCmd(generation uint64) tea.Cmd {
	ctx, repos := m.ctx, m.repos
	runtimeGeneration := m.studioRuntimeGeneration
	return func() tea.Msg {
		payload, err := loadHomePayload(ctx, repos)
		return homeReloadResultMsg{generation: generation, runtimeGeneration: runtimeGeneration, payload: payload, err: err}
	}
}

func (m *Model) applyHomeReload(msg homeReloadResultMsg) {
	if !m.onHome() || msg.generation != m.homeScreen.Generation() || msg.runtimeGeneration != m.studioRuntimeGeneration {
		return
	}
	m.homeScreen = m.homeScreen.Apply(home.Result{Generation: msg.generation, Payload: msg.payload, Err: msg.err})
	if msg.err != nil {
		m.status = msg.err.Error()
	} else {
		m.status = m.t("tui.status.refreshed")
	}
}

func loadHomePayload(ctx context.Context, repos Repositories) (home.Payload, error) {
	if repos.Projects == nil {
		return home.Payload{}, nil
	}
	projects, err := repos.Projects.ListProjects(ctx)
	if err != nil {
		return home.Payload{}, err
	}
	payload := home.Payload{Projects: projects, Tags: make(map[int64][]domain.Tag, len(projects)), Pending: make(map[int64]int, len(projects))}
	for _, project := range projects {
		if repos.Tags != nil {
			if tags, tagErr := repos.Tags.ListProjectTags(ctx, project.ID); tagErr == nil {
				payload.Tags[project.ID] = tags
			}
		}
		if repos.Tasks != nil {
			if count, countErr := countPendingTasks(ctx, repos, project.ID); countErr == nil {
				payload.Pending[project.ID] = count
			}
		}
	}
	return payload, nil
}

func countPendingTasks(ctx context.Context, repos Repositories, projectID int64) (int, error) {
	snapshot := repos.activeSnapshot()
	tasks, err := repos.Tasks.ListTasks(ctx, projectID, domain.TaskFilter{}, snapshot)
	if err != nil {
		return 0, err
	}
	if snapshot == nil || len(snapshot.Workflow().Buckets) == 0 {
		return len(tasks), nil
	}
	final := snapshot.Workflow().FinalBucketKey()
	count := 0
	for _, task := range tasks {
		if task.BucketKey != final {
			count++
		}
	}
	return count, nil
}

func (m *Model) selectHomeProjectID(projectID int64) bool {
	for _, project := range m.homeScreen.Projects() {
		if project.ID == projectID {
			if err := m.selectHomeProject(project); err != nil {
				m.status = err.Error()
				return false
			}
			return true
		}
	}
	return false
}

func (m *Model) selectHomeProject(project domain.Project) error {
	var runtime *agentruntime.ProjectRuntime
	var runtimePath string
	if m.repos.Cache != nil {
		var err error
		runtime, runtimePath, err = m.resolveProjectRuntime(project)
		if err != nil {
			return err
		}
	}
	m.invalidateStudioHookHistory()
	m.projectGeneration++
	m.viewChangeGeneration++
	m.project = project.Context()
	m.repos.ProjectID = project.ID
	m.lastProjectRoot = project.RootPath
	if runtime != nil {
		if err := m.bindProjectRuntime(runtime, runtimePath); err != nil {
			return err
		}
	} else {
		m.clearProjectRuntime()
	}
	if m.top == topHome {
		m.top, m.sub = topTasks, subBoard
	}
	m.boardScreen, m.tableScreen, m.graphScreen = board.New(), table.New(), graph.New()
	m.logsScreen = m.logsScreen.Reset()
	delete(m.dataVersionBaselines, realtimeReloadLogs)
	delete(m.lastAppliedReloadGen, realtimeReloadLogs)
	m.status = ""
	return m.refresh()
}

func (m *Model) resolveProjectRuntime(project domain.Project) (*agentruntime.ProjectRuntime, string, error) {
	cache := m.repos.Cache
	entry := cache.Get(project.ID)
	path := ""
	if entry != nil {
		path = entry.SourcePath
	}
	if path == "" {
		repoLocal, found, err := config.FindRepoLocal(project.RootPath)
		if err != nil {
			return nil, "", err
		}
		if found {
			path, err = paths.ActiveConfigFileInDir(filepath.Join(repoLocal, "config"))
			if err != nil {
				return nil, "", err
			}
		} else if m.repos.RepoLocalDir == "" {
			path = m.repos.ConfigPath
		}
	}
	if entry == nil && path == "" {
		return nil, "", nil
	}
	cache.SetProjectSelector(operation.ProjectSelector{ProjectID: project.ID})
	runtime, err := cache.Resolve(m.ctx, project.ID, path)
	return runtime, path, err
}

func (m *Model) bindProjectRuntime(pr *agentruntime.ProjectRuntime, path string) error {
	if pr == nil || pr.Snapshot == nil {
		return fmt.Errorf("tui: project selection returned an empty project runtime")
	}
	snap := pr.Snapshot
	settings := snap.Settings()
	if err := snap.ThemeError(); err != nil {
		return err
	}
	m.studioRuntimeGeneration++
	m.studioScreen = studio.New()
	m.studioApplyDiff = nil
	m.repos.Editor = pr.Editor
	if m.repos.Editor != nil && path != "" {
		m.repos.Editor.SetPath(path)
	}
	m.repos.ConfigPath = path
	m.repos.Catalog = snap.Catalog(config.SurfaceTUI)
	m.theme = snap.Theme()
	m.styles = newStyles(m.theme)
	m.priorities = snap.Priorities()
	m.severities = snap.Severities()
	m.registry = pr.EnumRegistry
	m.notifications = snap.Notifications()
	m.languages = settings.EffectiveLanguages()
	m.tokenBadgeYellow, m.tokenBadgeRed = settings.TUI.TokenBadge.Effective()
	return nil
}

func (m *Model) prepareHomeProjectDelete(projectID int64) {
	var project domain.Project
	for _, candidate := range m.homeScreen.Projects() {
		if candidate.ID == projectID {
			project = candidate
			break
		}
	}
	if project.ID == 0 || m.repos.Projects == nil {
		return
	}
	counters, err := m.repos.Projects.ProjectDeleteCounts(m.ctx, project.ID)
	if err != nil {
		counters = domain.ProjectDeleteCounters{}
	}
	m.homeScreen = m.homeScreen.ArmDelete(project.ID, counters)
	notif, ok := m.notifications["home-project-delete-confirm"]
	if !ok || err != nil {
		m.status = fmt.Sprintf(m.t("tui.confirm.home_project_delete_fmt"), project.Name)
		return
	}
	title := fmt.Sprintf(m.t("tui.notification.project_delete.title_fmt"), project.Name)
	body := fmt.Sprintf(m.t("tui.notification.project_delete.body_fmt"), counters.Tasks, counters.Comments, counters.Plans, counters.Tags, counters.ActivityLogEntries)
	model, _ := newNotification(notificationOptions{Notification: notif, Theme: m.theme, Text: title + "\n\n" + body, Catalog: m.repos.Catalog})
	m.notification = &model
}

func (m *Model) handleHomeProjectDeleteAction(action ActionMsg) tea.Cmd {
	if action.ActionID != "confirm" {
		m.homeScreen = m.homeScreen.CancelDelete()
		return nil
	}
	return m.applyScreenOutcome(m.homeScreen.ConfirmDelete())
}

func (m *Model) executeHomeProjectDeleteAction(action screenhost.Action) tea.Cmd {
	var project domain.Project
	for _, candidate := range m.homeScreen.Projects() {
		if candidate.ID == action.ProjectID {
			project = candidate
			break
		}
	}
	if project.ID == 0 {
		return nil
	}
	return m.executeHomeProjectDelete(project, action.DeleteCounters, action.Generation)
}

type homeProjectDeleteResultMsg struct {
	generation uint64
	project    domain.Project
	result     agentruntime.ProjectDeleteResult
	err        error
	audit      string
	pruneWarn  error
}

func (m *Model) executeHomeProjectDelete(project domain.Project, counters domain.ProjectDeleteCounters, generation uint64) tea.Cmd {
	var pruneWarn error
	backup, err := m.buildHomeBackupService(func(pruneErr error) { pruneWarn = pruneErr })
	if err != nil {
		m.status = err.Error()
		m.homeScreen = m.homeScreen.Apply(home.Result{Generation: generation, Err: err})
		return nil
	}
	if counters == (domain.ProjectDeleteCounters{}) {
		if refreshed, countErr := m.repos.Projects.ProjectDeleteCounts(m.ctx, project.ID); countErr == nil {
			counters = refreshed
		}
	}
	m.status = fmt.Sprintf(m.t("tui.status.deleting_project_fmt"), project.Name)
	ctx, repos := m.ctx, m.repos
	return func() tea.Msg {
		store, ok := repos.Projects.(agentruntime.ProjectStore)
		if !ok {
			return homeProjectDeleteResultMsg{generation: generation, project: project, err: fmt.Errorf("project store does not support delete")}
		}
		result, deleteErr := agentruntime.DeleteProjectChecked(ctx, store, backup, repos.Checkpointer, repos.Events, project.ID, counters)
		return homeProjectDeleteResultMsg{generation: generation, project: project, result: result, err: deleteErr, audit: result.Audit, pruneWarn: pruneWarn}
	}
}

func (m *Model) handleHomeProjectDeleteResult(msg homeProjectDeleteResultMsg) {
	if !m.onHome() || msg.generation != m.homeScreen.Generation() {
		return
	}
	if msg.err != nil {
		m.homeScreen = m.homeScreen.Apply(home.Result{Generation: msg.generation, Err: msg.err})
		m.status = msg.err.Error()
		drainAuditString(&m.status, msg.audit)
		return
	}
	payload, err := loadHomePayload(m.ctx, m.repos)
	m.homeScreen = m.homeScreen.Apply(home.Result{Generation: msg.generation, Payload: payload, Err: err})
	if err != nil {
		m.status = err.Error()
		drainAuditString(&m.status, msg.audit)
		return
	}
	m.status = fmt.Sprintf(m.t("tui.status.project_deleted_fmt"), msg.result.Project.Slug, msg.result.BackupPath)
	if msg.pruneWarn != nil {
		m.status += " · " + fmt.Sprintf(m.t("cli.db.backup.prune_warn_fmt"), msg.pruneWarn.Error())
	}
	drainAuditString(&m.status, msg.audit)
}

func drainAuditString(status *string, audit string) {
	var cleaned []string
	for _, part := range strings.Split(strings.TrimSpace(audit), "\n") {
		if part = strings.TrimSpace(part); part != "" {
			cleaned = append(cleaned, part)
		}
	}
	if len(cleaned) == 0 {
		return
	}
	if *status != "" {
		*status += " · "
	}
	*status += strings.Join(cleaned, " · ")
}

func (m *Model) buildHomeBackupService(pruneWarn func(error)) (*agentruntime.Backup, error) {
	destDir, err := paths.BackupDir()
	if err != nil {
		return nil, err
	}
	retention := 0
	if snapshot := m.repos.activeSnapshot(); snapshot != nil {
		retention = snapshot.Settings().Backup.RetentionCount
	}
	return agentruntime.NewBackup(agentruntime.BackupOptions{
		SourcePath:     m.repos.DBPath,
		DestDir:        destDir,
		Retention:      retention,
		PruneWarn:      pruneWarn,
		SnapshotWriter: m.repos.SnapshotWriter}), nil
}
