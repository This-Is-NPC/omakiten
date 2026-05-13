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
