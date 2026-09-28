package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/agentruntime"
	"omakiten/internal/config"
	"omakiten/internal/config/bundledraft"
	"omakiten/internal/configstore"
	"omakiten/internal/contract"
	"omakiten/internal/events"
	"omakiten/internal/testfixtures/bundleeditor"
	"omakiten/internal/testfixtures/snapstore"
	"omakiten/internal/token"
	"omakiten/internal/tui/screenhost"
)

func TestStudioApplyRebindsTheLiveRootForRepeatedSaves(t *testing.T) {
	m, editor, _, _ := newStudioApplyModel(t)
	enterStudioForApplyTest(t, &m)

	draft := m.studioScreen.State().Draft
	if draft == nil {
		t.Fatal("entering Studio opened no draft")
	}
	draft.RenameBucket(1, "first save")
	firstGeneration := m.studioRuntimeGeneration
	applyStudioTwiceForTest(t, &m)
	firstEditor := m.repos.Editor
	if m.studioRuntimeGeneration != firstGeneration+1 {
		t.Fatalf("generation after first save = %d, want %d", m.studioRuntimeGeneration, firstGeneration+1)
	}
	if firstEditor == editor || m.studioScreen.State().Draft != nil {
		t.Fatal("first save did not rebind the live root and clear Studio")
	}

	// Re-enter from the live root, not from the stale outcome screen returned
	// by the first apply. The second edit must use the newly wired editor.
	m.studioScreen = m.boundStudioScreenLive(screenhost.StudioCommands)
	secondDraft := m.studioScreen.State().Draft
	if secondDraft == nil {
		t.Fatal("second Studio entry opened no current draft")
	}
	secondDraft.RenameBucket(1, "second save")
	applyStudioTwiceForTest(t, &m)
	if m.studioRuntimeGeneration != firstGeneration+2 {
		t.Fatalf("generation after second save = %d, want %d", m.studioRuntimeGeneration, firstGeneration+2)
	}
	if m.repos.Editor == firstEditor || m.studioScreen.State().Draft != nil {
		t.Fatal("second save did not use the current runtime or clear Studio")
	}
}

func TestStudioApplyFailureDoesNotReportSuccessOrRotateRoot(t *testing.T) {
	m, _, _, _ := newStudioApplyModel(t)
	enterStudioForApplyTest(t, &m)
	draft := m.studioScreen.State().Draft
	if draft == nil {
		t.Fatal("entering Studio opened no draft")
	}
	// A missing cache makes the host reload fail after the draft write path;
	// bundledraft rolls the file back and Studio must retain the failure state.
	m.repos.Cache = nil
	m.studioScreen = m.boundStudioScreenLive(screenhost.StudioCommands)
	draft = m.studioScreen.State().Draft
	if draft == nil {
		t.Fatal("failure test opened no current draft")
	}
	draft.RenameBucket(1, "failed save")
	beforeGeneration := m.studioRuntimeGeneration
	applyStudioTwiceForTest(t, &m)
	if m.studioRuntimeGeneration != beforeGeneration {
		t.Fatal("failed Studio apply advanced the runtime generation")
	}
	message := m.studioScreen.State().ApplyMessage
	if !strings.Contains(message, "apply failed") || strings.Contains(message, "applied") {
		t.Fatalf("failed Studio apply message = %q, want failure without success", message)
	}
	if len(m.studioApplyDiff) == 0 {
		t.Fatal("failed Studio apply lost the candidate diff")
	}
	if strings.Contains(m.status, "applied") {
		t.Fatalf("failed Studio apply reported success in root status: %q", m.status)
	}
}

func newStudioApplyModel(t *testing.T) (Model, contract.BundleEditor, *snapstore.Store, events.Bus) {
	t.Helper()
	ctx := context.Background()
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	if err := config.SaveFullBundle(configPath, tuiTestBundle(t)); err != nil {
		t.Fatalf("SaveFullBundle: %v", err)
	}
	writeThemeFile(t, filepath.Join(tmp, "themes", "catppuccin.yaml"), "catppuccin", "Catppuccin")
	store := snapstore.Open(t, filepath.Join(tmp, "omakiten.db"))
	files := configstore.New()
	editor := bundleeditor.New(files, configPath)
	if _, err := bundledraft.ApplyPlanned(ctx, editor, nil); err != nil {
		t.Fatalf("editor.Apply: %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	bus := events.NewInProcessBus(config.EventsSettings{})
	cache := agentruntime.NewBundleCache(store.Store, bus, files)
	if _, err := cache.Resolve(ctx, project.ID, configPath); err != nil {
		t.Fatalf("cache.Resolve: %v", err)
	}
	m, err := NewModel(ctx, project.Context(), Repositories{
		Tasks: store, Comments: store, Dependencies: store, Editor: editor,
		BundleStore: files, Events: store, Orphans: store, Cache: cache,
		ProjectID: project.ID, ConfigPath: configPath,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	return m, editor, store, bus
}

func enterStudioForApplyTest(t *testing.T, m *Model) {
	t.Helper()
	m.navigation = screenhost.StudioCommands
	m.refreshAfterViewChangeCmd(screenhost.TasksBoard)
	if !m.studioScreen.StudioDraftOpen() {
		t.Fatal("Studio navigation did not retain the entered draft")
	}
}

func applyStudioTwiceForTest(t *testing.T, m *Model) {
	t.Helper()
	ctrlS := tea.KeyMsg{Type: tea.KeyCtrlS}
	if _, handled := m.dispatchOwnedScreenKey(ctrlS); !handled {
		t.Fatal("Studio did not handle first ctrl+s")
	}
	if _, handled := m.dispatchOwnedScreenKey(ctrlS); !handled {
		t.Fatal("Studio did not handle second ctrl+s")
	}
}
