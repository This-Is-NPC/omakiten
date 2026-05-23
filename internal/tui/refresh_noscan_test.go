package tui

import (
	"context"
	"path/filepath"
	"testing"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/configstore"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
	"omakiten/internal/testfixtures/runtimecache"
	"omakiten/internal/testfixtures/snapstore"
	"omakiten/internal/token"
)

// loadCountingBundleStore wraps a real configstore.Adapter and panics
// the moment any read path reaches LoadBundle. Used by the refresh
// hot-path tests to prove the TUI never re-walks disk on a tick.
type loadCountingBundleStore struct {
	inner app.BundleStore
}

func (s *loadCountingBundleStore) LoadBundle(path string) (config.Bundle, error) {
	panic("LoadBundle called: TUI refresh must read from the cached snapshot, not the bundle editor")
}

func (s *loadCountingBundleStore) SaveBundle(path string, bundle config.Bundle) error {
	return s.inner.SaveBundle(path, bundle)
}

func (s *loadCountingBundleStore) HashFile(path string) (string, error) {
	return s.inner.HashFile(path)
}

func (s *loadCountingBundleStore) WriteAtomic(path string, data []byte) error {
	return s.inner.WriteAtomic(path, data)
}

func (s *loadCountingBundleStore) EnsureDefaultFiles(rootDir string) error {
	return s.inner.EnsureDefaultFiles(rootDir)
}

func (s *loadCountingBundleStore) MigrateLayout(rootDir string) error {
	return s.inner.MigrateLayout(rootDir)
}

func (s *loadCountingBundleStore) ConfigRootFromYAMLPath(path string) string {
	return s.inner.ConfigRootFromYAMLPath(path)
}

// noScanBundle is the minimal fixture for refresh hot-path coverage:
// one skill, persona, law, and the default 2-bucket workflow. Inlined
// here (rather than reusing tuiTestBundle) so the helper accepts
// testing.TB and benchmarks can share the setup.
func noScanBundle(tb testing.TB) config.Bundle {
	tb.Helper()
	bundle, _ := testfixtures.LoadBundle(tb, "default_workflow.yaml")
	bundle.Skills = []config.Skill{{Slug: "go", Name: "Go"}}
	bundle.Personas = []config.Persona{{Slug: "agent", Name: "Agent", Skills: []string{"go"}}}
	bundle.Laws = []config.Law{{Slug: "scope", Severity: "error", Body: "Stay in scope.", Scope: "global"}}
	return bundle
}

// buildRefreshHotPathModel materialises a TUI Model whose Editor will
// panic if anything in the refresh path tries to LoadBundle. Returns
// the seeded model ready for refresh() drives.
func buildRefreshHotPathModel(tb testing.TB) Model {
	tb.Helper()
	tmp := tb.TempDir()
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	dbPath := filepath.Join(tmp, "omakiten.db")

	if err := config.SaveFullBundle(configPath, noScanBundle(tb)); err != nil {
		tb.Fatalf("SaveFullBundle: %v", err)
	}

	ctx := context.Background()
	store := snapstore.Open(tb, dbPath)

	files := configstore.New()
	editor := app.NewBundleEditor(files, configPath)
	resolved, err := editor.Apply(ctx, nil)
	if err != nil {
		tb.Fatalf("editor.Apply seed: %v", err)
	}
	if err := store.ImportBundle(ctx, resolved, configPath, ""); err != nil {
		tb.Fatalf("ImportBundle: %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		tb.Fatalf("UpsertProject: %v", err)
	}
	if _, err := store.CreateTask(ctx, project.ID, "Task", "", domain.Priority(2), "backlog", nil, store.Snapshot()); err != nil {
		tb.Fatalf("CreateTask: %v", err)
	}

	// Swap in the panicking BundleStore after the initial seed Apply so
	// any subsequent refresh-driven LoadBundle blows up loudly.
	noScanEditor := app.NewBundleEditor(&loadCountingBundleStore{inner: files}, configPath)

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Editor:       noScanEditor,
		BundleStore:  files,
		EntityFiles:  files,
		Slugger:      files,
		Catalog:      newTestCatalog(tb),
		Cache:        runtimecache.Install(0, store.Snapshot()),
		Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		tb.Fatalf("NewModel: %v", err)
	}
	return model
}

// TestModelRefreshNeverLoadsBundleFromDisk pins the perf invariant: a
// refresh tick must source every entity slice from the cached snapshot
// and never round-trip through the editor's BundleStore. Regressing
// either branch (activeViewSettings or TUIQueryService.Snapshot) would
// trip the panic store and fail the test.
func TestModelRefreshNeverLoadsBundleFromDisk(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	if err := model.refresh(); err != nil {
		t.Fatalf("refresh(): %v", err)
	}
	// activeViewSettings used to call editor.Load() inline; drive it
	// directly to cover the second hot-path read.
	_ = model.activeViewSettings()
}

// BenchmarkModelRefreshHotPath measures one refresh tick with the
// panic-store editor in place — any disk-scan regression would panic
// rather than silently slow down. The headline number is ns/op for a
// fixed bundle shape (1 task, 1 skill, 1 law, 1 persona); use it as
// the comparison baseline when adding W3 render caches.
func BenchmarkModelRefreshHotPath(b *testing.B) {
	model := buildRefreshHotPathModel(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := model.refresh(); err != nil {
			b.Fatalf("refresh(): %v", err)
		}
	}
}
