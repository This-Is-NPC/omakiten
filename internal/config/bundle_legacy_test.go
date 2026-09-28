package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadBundleRejectsLegacyKeysWithoutRewriting(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	legacy := []byte("version: 1\nconfig:\n  views:\n    logs:\n      filter: {source: [cli]}\n  activity_log: {max_rows: 500, max_age_days: 7}\n")
	legacy = append(legacy, []byte("personas:\n  - slug: old\n    skills: [go]\n")...)
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(configPath, legacy, 0o600); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	_, err := LoadBundle(configPath)
	if err == nil || !strings.Contains(err.Error(), "field filter not found") {
		t.Fatalf("LoadBundle() error = %v, want explicit logs.filter.source rejection", err)
	}
	got, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("read rejected config: %v", readErr)
	}
	if !bytes.Equal(got, legacy) {
		t.Fatalf("rejected config was rewritten:\nwant %q\n got %q", legacy, got)
	}
}
