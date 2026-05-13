package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/paths"
)

func TestRevertConfigSwap_NoPendingPathIsNoOp(t *testing.T) {
	m, _ := newPickerModel(t)
	beforePath := m.repos.Editor.Path()
	m.revertConfigSwap()
	if m.repos.Editor.Path() != beforePath {
		t.Fatalf("Path mutated despite empty pending revert: was %q, now %q", beforePath, m.repos.Editor.Path())
	}
	if m.status != "" {
		t.Fatalf("status changed without a pending revert: %q", m.status)
	}
}

func TestRevertConfigSwap_RestoresPreviousActiveConfig(t *testing.T) {
	m, root := newPickerModel(t)
	t.Setenv(paths.HomeEnv, root)
	t.Setenv("XDG_CONFIG_HOME", "")

	originalPath := m.repos.Editor.Path()
	originalThemeKey := m.theme.Key

	// Pretend a swap to the experiment yaml already happened: repoint
	// the editor at the experiment file and stash the previous path on
	// the model so revert has something to fall back to.
	experimentPath := filepath.Join(root, "config", "custom", "config-experiment.yaml")
	if _, err := m.repos.BundleStore.LoadBundle(originalPath); err != nil {
		// sanity: the original bundle must still load — otherwise revert
		// cannot succeed and the test would fail for the wrong reason.
		t.Fatalf("baseline bundle invalid: %v", err)
	}
	m.repos.Editor.SetPath(experimentPath)
	m.pendingSwapRevertPath = originalPath

	m.revertConfigSwap()

	if m.pendingSwapRevertPath != "" {
		t.Fatalf("pendingSwapRevertPath not cleared after revert: %q", m.pendingSwapRevertPath)
	}
	if m.repos.Editor.Path() != originalPath {
		t.Fatalf("editor.Path after revert = %q, want %q", m.repos.Editor.Path(), originalPath)
	}
	if !strings.Contains(strings.ToLower(m.status), "cancelled") {
		t.Fatalf("status should announce cancel, got %q", m.status)
	}
	if m.theme.Key != originalThemeKey {
		t.Fatalf("theme.Key after revert = %q, want %q", m.theme.Key, originalThemeKey)
	}
}
