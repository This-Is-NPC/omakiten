package testfixtures_test

import (
	"os"
	"path/filepath"
	"testing"

	"omakiten/internal/testfixtures"
)

// TestLoadBundleStrictRejectsUnknownFields proves the loader runs in
// KnownFields(true) mode. Without strict decoding a typo (e.g. `defualts`
// instead of `defaults`) would silently parse to an empty struct and the
// test using the fixture would pass for the wrong reason — exactly the
// failure mode the strict mode is meant to catch.
func TestLoadBundleStrictRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "typo.yaml")
	yaml := []byte(`version: 1
kit:
  id: 1
  key: x
  name: X
config:
  workflow:
    active: default
  theme:
    active: catppuccin
workflows:
  - id: 1
    key: default
    name: Default
    defualts:   # typo — should fail strict decoding
      task: { edit: false }
    buckets:
      - id: 1
        key: backlog
        name: Backlog
        position: 1
`)
	if err := os.WriteFile(path, yaml, 0o644); err != nil {
		t.Fatalf("WriteFile = %v", err)
	}

	// LoadBundleFromAbsPath funnels through the same loader as LoadBundle.
	// We expect the test to be marked failed inside LoadBundle's t.Fatalf,
	// so we run it in a sub-test and inspect the result via the harness.
	subT := &capturingT{T: t}
	defer func() { _ = recover() }()
	_, _ = testfixtures.LoadBundleFromAbsPath(subT, path)
	if !subT.failed {
		t.Fatal("LoadBundleFromAbsPath accepted a fixture with an unknown key; strict decoding is not active")
	}
}

// capturingT records whether t.Fatalf was called so the test above can
// verify LoadBundleFromAbsPath rejected the bad input. testing.T does
// not expose a "did Fatal fire?" hook, so we wrap it with a minimal
// recorder that satisfies the testing.TB-shaped surface LoadBundle uses.
type capturingT struct {
	*testing.T
	failed bool
}

func (c *capturingT) Fatalf(format string, args ...any) {
	c.failed = true
	// Stop the goroutine the way testing.T.Fatalf does, so the caller
	// returns immediately. The deferred recover above swallows it.
	panic("capturingT.Fatalf")
}

func (c *capturingT) Helper() {}
