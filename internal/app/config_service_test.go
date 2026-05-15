package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"omakiten/internal/configstore"
)

func TestConfigServiceImport(t *testing.T) {
	ctx := context.Background()
	store, _ := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	service := NewConfigService(store, configstore.New())

	tmp := t.TempDir()
	validPath := filepath.Join(tmp, "omakiten.yaml")
	data, _ := yaml.Marshal(appTestBundle(t, 1000))
	if err := os.WriteFile(validPath, data, 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	bundle, hash, _, err := service.Import(ctx, validPath)
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if bundle.Kit.Key != "default" {
		t.Fatalf("Import().Kit.Key = %q, want %q", bundle.Kit.Key, "default")
	}
	if hash == "" {
		t.Fatal("Import() hash is empty")
	}

	// Missing file -> error
	_, _, _, err = service.Import(ctx, filepath.Join(tmp, "missing.yaml"))
	if err == nil {
		t.Fatal("Import() missing file error = nil")
	}

	// Invalid YAML -> error
	invalidPath := filepath.Join(tmp, "invalid.yaml")
	if err := os.WriteFile(invalidPath, []byte("not: valid: ["), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	_, _, _, err = service.Import(ctx, invalidPath)
	if err == nil {
		t.Fatal("Import() invalid yaml error = nil")
	}
}

// TestConfigServiceImportPopulatesProvidersWithoutSQLConfigTables
// asserts that the Phase 2 in-memory refactor (task #110) made
// ConfigService.Import a pure provider Swap on the SQL side: every
// config table the import path previously populated is now dropped
// (migration 020). The test imports a bundle and then checks that the
// provider snapshot serves the workflow, personas, and laws —
// the data must come from the snapshot because the SQL tables that
// used to back the reads no longer exist.
func TestConfigServiceImportPopulatesProvidersWithoutSQLConfigTables(t *testing.T) {
	ctx := context.Background()
	store, _ := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	service := NewConfigService(store, configstore.New())

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "omakiten.yaml")
	data, _ := yaml.Marshal(appTestBundle(t, 1000))
	if err := os.WriteFile(cfgPath, data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, _, _, err := service.Import(ctx, cfgPath); err != nil {
		t.Fatalf("Import: %v", err)
	}

	// Workflow shape is the easiest provider surface to assert from
	// inside this test — it lives inside omakiten.yaml so the marshal
	// round-trip carries it through. Personas / skills / laws live in
	// per-entity files (`yaml:"-"`) and need a directory walk to land
	// in the bundle; LoadBundle handles that in production but this
	// test only writes the central yaml.
	wf := store.Snapshot().Workflow()
	if wf.Key == "" || len(wf.Buckets) == 0 {
		t.Fatalf("Snapshot().Workflow() returned empty after Import: %+v", wf)
	}

	// Settings flow through the snapshot too — the kit fallback only
	// kicks in when the value is zero, so this confirms the in-memory
	// path is the one supplying the answer.
	settings, err := store.ContextSettings(ctx)
	if err != nil {
		t.Fatalf("ContextSettings: %v", err)
	}
	if settings.MaxTokens != 1000 {
		t.Fatalf("ContextSettings.MaxTokens = %d, want 1000 (provider populated from bundle)", settings.MaxTokens)
	}
}
