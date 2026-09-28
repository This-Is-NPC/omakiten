package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/commandcatalog"
	"omakiten/internal/config/bundledraft"
	"omakiten/internal/contract"
	"omakiten/internal/studioprojection"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/studio"
)

func (m Model) boundStudioScreen(id screenhost.ID) studio.Screen {
	deps := m.studioDependencies()
	deps.Reload = studioReloadHost(&m)
	return m.studioScreen.Bind(id, deps)
}

// boundStudioScreenLive is the mutating host path. Bubble Tea owns Model as a
// value, but this method is called from pointer-based dispatch so a Studio
// apply can update the same root copy that will be returned from Update.
func (m *Model) boundStudioScreenLive(id screenhost.ID) studio.Screen {
	deps := m.studioDependencies()
	deps.Reload = studioReloadHost(m)
	return m.studioScreen.Bind(id, deps)
}

func studioReloadHost(m *Model) func(string, []string) error {
	return func(path string, diff []string) error {
		m.studioApplyDiff = append([]string(nil), diff...)
		return m.reloadBundle(path)
	}
}

func (m Model) studioDependencies() studio.Deps {
	return studio.Deps{
		Ctx:               m.ctx,
		Editor:            m.repos.Editor,
		ProjectID:         m.project.ID,
		RuntimeGeneration: m.studioRuntimeGeneration,
		OpenDraft:         openStudioBundleDraft,
		Snapshot:          m.repos.activeSnapshot(),
		Catalog:           m.repos.Catalog,
		Workflow:          m.workflow,
		Tasks:             m.tasks,
		ConfigPath:        m.repos.ConfigPath,
		CommandNames:      commandcatalog.CommandNames(),
		ResolveCommand:    m.resolveStudioCommand,
		HookHistory:       m.studioHookHistory,
	}
}

type studioHookHistoryResultMsg struct {
	projectID  int64
	generation uint64
	history    map[int][]studioprojection.HookExecuted
	err        error
}

func (m *Model) prepareStudioHookHistory() tea.Cmd {
	if m.navigation != screenhost.StudioWorkflow && m.navigation != screenhost.StudioCommands && m.navigation != screenhost.StudioPersonas && m.navigation != screenhost.StudioHooks {
		return nil
	}
	projectID := m.project.ID
	if m.studioHookHistoryReady && m.studioHookHistoryProjectID == projectID {
		return nil
	}
	if m.studioHookHistoryLoading {
		return nil
	}
	if m.repos.Events == nil || projectID == 0 {
		m.studioHookHistory = nil
		m.studioHookHistoryProjectID = projectID
		m.studioHookHistoryReady = false
		return nil
	}
	m.studioHookHistory = nil
	m.studioHookHistoryProjectID = projectID
	m.studioHookHistoryReady = false
	m.studioHookHistoryLoading = true
	generation := m.studioHookHistoryGeneration
	ctx, events := m.ctx, m.repos.Events
	return func() tea.Msg {
		history, err := studioprojection.LoadHookHistory(ctx, events, projectID, studioHookHistoryLimit)
		return studioHookHistoryResultMsg{projectID: projectID, generation: generation, history: history, err: err}
	}
}

func (m *Model) invalidateStudioHookHistory() {
	m.studioHookHistory = nil
	m.studioHookHistoryReady = false
	m.studioHookHistoryProjectID = 0
	m.studioHookHistoryLoading = false
	m.studioHookHistoryGeneration++
}

func (m *Model) applyStudioHookHistory(msg studioHookHistoryResultMsg) {
	if msg.generation != m.studioHookHistoryGeneration || msg.projectID != m.project.ID {
		return
	}
	m.studioHookHistoryLoading = false
	if msg.err != nil {
		m.studioHookHistory = nil
		m.studioHookHistoryReady = false
		m.status = msg.err.Error()
		return
	}
	m.studioHookHistory = msg.history
	m.studioHookHistoryProjectID = msg.projectID
	m.studioHookHistoryReady = true
}

// openStudioBundleDraft is the host half of the screen's draft port: Studio
// declares what a draft does, the host says which engine does it. Keeping the
// choice here is what lets the screen paint a candidate without linking the
// mutation and validation logic that produced it.
func openStudioBundleDraft(editor contract.BundleEditor) (studio.StudioDraft, error) {
	return bundledraft.New(editor)
}
