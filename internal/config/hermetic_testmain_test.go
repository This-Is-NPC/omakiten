package config

import (
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	testHome, err := os.MkdirTemp("", "omakiten-config-test-home-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "config testmain temp home: %v\n", err)
		os.Exit(1)
	}
	// Keep t.TempDir roots below an isolated home so repo-local discovery
	// cannot walk into an external /tmp/.omakiten install.
	if err := os.Setenv("HOME", testHome); err != nil {
		fmt.Fprintf(os.Stderr, "config testmain HOME: %v\n", err)
		os.Exit(1)
	}
	if err := os.Setenv("TMPDIR", testHome); err != nil {
		fmt.Fprintf(os.Stderr, "config testmain TMPDIR: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(testHome)
	os.Exit(code)
}
