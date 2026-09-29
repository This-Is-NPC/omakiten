package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"omakiten/internal/testfixtures"
)

// TestMain isolates config, data, cache and state paths from the host installation.
func TestMain(m *testing.M) {
	testHome, err := os.MkdirTemp("", "omakiten-cli-test-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("cli testmain temp home: %w", err))
		os.Exit(1)
	}
	// Keep t.TempDir roots below an isolated home so repo-local discovery
	// cannot walk into an external /tmp/.omakiten install.
	if err := os.Setenv("HOME", testHome); err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("cli testmain HOME: %w", err))
		os.Exit(1)
	}
	if err := os.Setenv("TMPDIR", testHome); err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("cli testmain TMPDIR: %w", err))
		os.Exit(1)
	}
	for _, directory := range []string{"CONFIG", "DATA", "CACHE", "STATE"} {
		if err := os.Setenv("XDG_"+directory+"_HOME", filepath.Join(testHome, directory)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	gitConfig, err := testfixtures.PresetGitConfig(testHome)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.Setenv("GIT_CONFIG_GLOBAL", gitConfig); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(testHome)
	os.Exit(code)
}
