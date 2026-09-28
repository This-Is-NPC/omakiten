package tui

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"omakiten/internal/agentruntime"
	"omakiten/internal/config"
	"omakiten/internal/configstore"
	"omakiten/internal/domain"
	"omakiten/internal/events"
	"omakiten/internal/testfixtures/bundleeditor"
	"omakiten/internal/testfixtures/snapstore"
	"omakiten/internal/token"
	"omakiten/internal/tui/screens/studio"
)

// TestReloadBundleUsesCacheWhenWired asserts the Phase 3e routing: when
// the Model carries a non-nil Repositories.Cache, reloadBundle delegates
// to cache.Reload instead of ConfigService.Import. The proof is twofold
// — the cache entry's pointer rotates (Reload always rebuilds) and the
// new entry's bundle reflects the on-disk edit. A regression that
// silently falls back to ConfigService.Import would either keep the
// cache pointer or skip the rotated bundle entirely.
func TestReloadBundleUsesCacheWhenWired(t *testing.T) {
	fixture := newReloadBundleFixture(t)
	ctx, configPath, store, files := fixture.ctx, fixture.configPath, fixture.store, fixture.files
	project, editor, cache := fixture.project, fixture.editor, fixture.cache
	firstEntry := cache.Get(project.ID)
	if firstEntry == nil {
		t.Fatal("cache.Get(project.ID) nil after Resolve")
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Comments:     store,
		Dependencies: store,

		Editor:      editor,
		BundleStore: files,
		Events:      store,
		Orphans:     store,
		Cache:       cache,
		ProjectID:   project.ID,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	draft := dirtyStudioDraft(t, editor, "dirty before reload")
	model.studioScreen = model.studioScreen.WithState(studio.State{Draft: draft, ApplyArmed: true, ApplyConfirmation: "confirmed-before-reload"})

	// Bump mtime so cache.Reload sees a change to confirm it rebuilt
	// (Reload bypasses mtime, but advancing it also catches a regression
	// where Reload is silently replaced with Resolve in the future).
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(configPath, future, future); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	if err := model.reloadBundle(configPath); err != nil {
		t.Fatalf("reloadBundle: %v", err)
	}
	if state := model.studioScreen.State(); state.Draft != nil || state.ApplyArmed || state.ApplyConfirmation != "" {
		t.Fatalf("reloadBundle retained Studio state: %+v", state)
	}

	secondEntry := cache.Get(project.ID)
	if secondEntry == nil {
		t.Fatal("cache.Get(project.ID) nil after reloadBundle")
	}
	if secondEntry == firstEntry {
		t.Fatal("cache pointer did not rotate — reloadBundle bypassed cache.Reload")
	}
	if model.registry == nil {
		t.Fatal("model.registry nil after reload — provider snapshot did not propagate")
	}
	if secondEntry.EnumRegistry != model.registry {
		t.Fatalf("model.registry (%p) != cache entry registry (%p) — model state out of sync with cache",
			model.registry, secondEntry.EnumRegistry)
	}
}

func TestReloadBundleIfChangedAppliesMtimeReload(t *testing.T) {
	fixture := newReloadBundleFixture(t)
	ctx, configPath, store, files := fixture.ctx, fixture.configPath, fixture.store, fixture.files
	project, editor, cache := fixture.project, fixture.editor, fixture.cache
	firstEntry := cache.Get(project.ID)

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Comments:     store,
		Dependencies: store,
		Editor:       editor,
		BundleStore:  files,
		Cache:        cache,
		ProjectID:    project.ID,
		ConfigPath:   configPath,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	draft := dirtyStudioDraft(t, editor, "dirty before hot reload")
	model.studioScreen = model.studioScreen.WithState(studio.State{Draft: draft, ApplyArmed: true, ApplyConfirmation: "confirmed-before-hot-reload"})
	if model.languages.AgentOutput != "" {
		t.Fatalf("initial AgentOutput = %q, want empty", model.languages.AgentOutput)
	}

	bundle, err := config.LoadBundle(configPath)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	bundle.Config.Languages.AgentOutput = "Português (Brasil)"
	if err := config.SaveBundle(configPath, bundle); err != nil {
		t.Fatalf("SaveBundle: %v", err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(configPath, future, future); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	changed, err := model.reloadBundleIfChanged()
	if err != nil {
		t.Fatalf("reloadBundleIfChanged: %v", err)
	}
	if !changed {
		t.Fatal("reloadBundleIfChanged reported no change after config mtime advanced")
	}
	if cache.Get(project.ID) == firstEntry {
		t.Fatal("cache pointer did not rotate after mtime reload")
	}
	if model.languages.AgentOutput != "Português (Brasil)" {
		t.Fatalf("model AgentOutput = %q, want Português (Brasil)", model.languages.AgentOutput)
	}
	if state := model.studioScreen.State(); state.Draft != nil || state.ApplyArmed || state.ApplyConfirmation != "" {
		t.Fatalf("hot reload retained Studio state: %+v", state)
	}
}

func dirtyStudioDraft(t *testing.T, editor BundleEditor, name string) StudioDraft {
	t.Helper()
	draft, err := NewStudioDraft(editor)
	if err != nil {
		t.Fatalf("NewStudioDraft: %v", err)
	}
	draft.RenameBucket(1, name)
	return draft
}

type reloadBundleFixture struct {
	ctx        context.Context
	configPath string
	store      *snapstore.Store
	files      *configstore.Adapter
	editor     BundleEditor
	project    domain.Project
	cache      *agentruntime.BundleCache
}

func newReloadBundleFixture(t *testing.T) reloadBundleFixture {
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
	if _, err := applyBundleEditor(ctx, editor, nil); err != nil {
		t.Fatalf("editor.Apply: %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	cache := agentruntime.NewBundleCache(store.Store, events.NewInProcessBus(config.EventsSettings{}), files)
	if _, err := cache.Resolve(ctx, project.ID, configPath); err != nil {
		t.Fatalf("cache.Resolve initial: %v", err)
	}
	return reloadBundleFixture{ctx: ctx, configPath: configPath, store: store, files: files, editor: editor, project: project, cache: cache}
}
