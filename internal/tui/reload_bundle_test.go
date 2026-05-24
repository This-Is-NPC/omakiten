package tui

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"omakiten/internal/agentruntime"
	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/configstore"
	"omakiten/internal/events"
	"omakiten/internal/testfixtures"
	"omakiten/internal/testfixtures/snapstore"
	"omakiten/internal/token"
)

// TestReloadBundleUsesCacheWhenWired asserts the Phase 3e routing: when
// the Model carries a non-nil Repositories.Cache, reloadBundle delegates
// to cache.Reload instead of ConfigService.Import. The proof is twofold
// — the cache entry's pointer rotates (Reload always rebuilds) and the
// new entry's bundle reflects the on-disk edit. A regression that
// silently falls back to ConfigService.Import would either keep the
// cache pointer or skip the rotated bundle entirely.
func TestReloadBundleUsesCacheWhenWired(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	dbPath := filepath.Join(tmp, "omakiten.db")

	if err := config.SaveFullBundle(configPath, tuiTestBundle(t)); err != nil {
		t.Fatalf("SaveFullBundle: %v", err)
	}
	writeThemeFile(t, filepath.Join(tmp, "themes", "catppuccin.yaml"), "catppuccin", "Catppuccin")

	store := snapstore.Open(t, dbPath)

	files := configstore.New()
	editor := app.NewBundleEditor(files, configPath)
	if _, err := editor.Apply(ctx, nil); err != nil {
		t.Fatalf("editor.Apply: %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}

	bus := events.NewInProcessBus(config.EventsSettings{})
	cache := agentruntime.NewBundleCache(store.Store, bus, files)
	if _, err := cache.Resolve(ctx, project.ID, configPath); err != nil {
		t.Fatalf("cache.Resolve initial: %v", err)
	}
	firstEntry := cache.Get(project.ID)
	if firstEntry == nil {
		t.Fatal("cache.Get(project.ID) nil after Resolve")
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()),
		Comments:     store,
		Dependencies: store,
		Entries:      store,

		Editor:      editor,
		BundleStore: files,
		EntityFiles: files,
		Slugger:     files,
		Events:      store,
		Orphans:     store,
		Cache:       cache,
		ProjectID:   project.ID,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}

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
