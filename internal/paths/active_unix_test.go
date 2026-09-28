//go:build aix || darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd || solaris

package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func runMarkerSwapDuringValidation(t *testing.T, marker, victim string, operation func() error) error {
	t.Helper()
	validationStarted := make(chan struct{})
	swapResult := make(chan error, 1)
	var hookErr error

	activeMarkerValidationHook = func() {
		close(validationStarted)
		select {
		case hookErr = <-swapResult:
		case <-time.After(2 * time.Second):
			hookErr = fmt.Errorf("marker swap did not complete during validation window")
		}
	}
	defer func() { activeMarkerValidationHook = func() {} }()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-validationStarted
		if err := os.Remove(marker); err != nil {
			swapResult <- fmt.Errorf("remove marker for adversarial swap: %w", err)
			return
		}
		if err := os.Symlink(victim, marker); err != nil {
			swapResult <- fmt.Errorf("install symlink for adversarial swap: %w", err)
			return
		}
		swapResult <- nil
	}()

	err := operation()
	wg.Wait()
	if hookErr != nil {
		t.Fatalf("synchronize adversarial marker swap: %v", hookErr)
	}
	return err
}

func requireSymlinkSupport(t *testing.T, dir, victim string) {
	t.Helper()
	probe := filepath.Join(dir, ".symlink-probe")
	if err := os.Symlink(victim, probe); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if err := os.Remove(probe); err != nil {
		t.Fatalf("remove symlink probe: %v", err)
	}
}

func TestSetActiveConfigRejectsSynchronizedMarkerSwap(t *testing.T) {
	requireActiveMarkerBackend(t)
	dir := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim")
	marker := filepath.Join(dir, ActiveConfigStateFile)
	requireSymlinkSupport(t, dir, victim)
	if err := os.WriteFile(victim, []byte("safe\n"), 0o600); err != nil {
		t.Fatalf("write victim: %v", err)
	}
	if err := os.WriteFile(marker, []byte("old.yaml\n"), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	err := runMarkerSwapDuringValidation(t, marker, victim, func() error {
		return SetActiveConfigInDir(dir, "new.yaml")
	})
	if err == nil || !strings.Contains(err.Error(), "symlink appeared during write") {
		t.Fatalf("SetActiveConfigInDir error = %v, want synchronized identity rejection", err)
	}
	got, err := os.ReadFile(victim)
	if err != nil {
		t.Fatalf("read victim: %v", err)
	}
	if string(got) != "safe\n" {
		t.Fatalf("victim changed during marker replacement race: %q", got)
	}
}
