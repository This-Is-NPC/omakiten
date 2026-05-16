package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"omakiten/internal/config"
	"omakiten/internal/configstore"
)

func TestConfigServiceImport(t *testing.T) {
	ctx := context.Background()

	service := NewConfigService(configstore.New())

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

// TestConfigServiceImportReturnsParsedBundleAndRegistry asserts that
// Import is now a pure read: it parses the YAML, builds the
// instance-scoped EnumRegistry, and returns both. Phase 2-bis dropped
// the Store write-back; downstream rotation is driven by
// BundleCache.Reload on mtime change. The returned Snapshot, built
// from the bundle, serves the workflow shape without touching the SQL
// adapter.
func TestConfigServiceImportReturnsParsedBundleAndRegistry(t *testing.T) {
	ctx := context.Background()

	service := NewConfigService(configstore.New())

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "omakiten.yaml")
	data, _ := yaml.Marshal(appTestBundle(t, 1000))
	if err := os.WriteFile(cfgPath, data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	bundle, _, registry, err := service.Import(ctx, cfgPath)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if registry == nil {
		t.Fatal("Import: registry is nil; Import must build the EnumRegistry from the bundle")
	}
	snap := config.BuildSnapshot(bundle)
	wf := snap.Workflow()
	if wf.Key == "" || len(wf.Buckets) == 0 {
		t.Fatalf("Snapshot().Workflow() returned empty after Import: %+v", wf)
	}

	settings := snap.ContextSettings()
	if settings.MaxTokens != 1000 {
		t.Fatalf("Snapshot().ContextSettings().MaxTokens = %d, want 1000", settings.MaxTokens)
	}
}
