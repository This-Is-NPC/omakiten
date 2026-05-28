package installer

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestWriteActivePreset_HonoursOmakitenHome pins the resolver to a
// tmpdir via OMAKITEN_HOME (the env knob paths.ConfigRoot consults
// first) and asserts the .active file lands at the same place
// install.sh's write_active_preset wrote to.
func TestWriteActivePreset_HonoursOmakitenHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("OMAKITEN_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", "")

	dir, err := WriteActivePreset("kaiseki")
	if err != nil {
		t.Fatalf("WriteActivePreset: %v", err)
	}
	wantDir := filepath.Join(tmp, "config")
	if dir != wantDir {
		t.Fatalf("config dir: got %q want %q", dir, wantDir)
	}
	got, err := os.ReadFile(filepath.Join(dir, ".active"))
	if err != nil {
		t.Fatalf("read .active: %v", err)
	}
	if string(got) != "kaiseki.yaml\n" {
		t.Fatalf(".active contents: got %q want %q", got, "kaiseki.yaml\n")
	}
}

func TestWriteActivePreset_RejectsEmpty(t *testing.T) {
	t.Setenv("OMAKITEN_HOME", t.TempDir())
	if _, err := WriteActivePreset(""); err == nil {
		t.Fatalf("expected error on empty preset")
	}
}

// TestWriteWrappers_SkipsMissingRC mirrors install.sh's behaviour: only
// touch rc files that already exist; an absent shell rc is fine.
func TestWriteWrappers_SkipsMissingRC(t *testing.T) {
	home := t.TempDir()
	bashrc := filepath.Join(home, ".bashrc")
	if err := os.WriteFile(bashrc, []byte("# existing\n"), 0o644); err != nil {
		t.Fatalf("seed bashrc: %v", err)
	}
	// .zshrc deliberately missing.

	installed, err := WriteWrappers(home)
	if err != nil {
		t.Fatalf("WriteWrappers: %v", err)
	}
	if diff := cmp.Diff([]string{bashrc}, installed); diff != "" {
		t.Fatalf("installed mismatch (-want +got):\n%s", diff)
	}
	if _, err := os.Stat(filepath.Join(home, ".zshrc")); err == nil {
		t.Fatalf("WriteWrappers created .zshrc when seed was missing")
	}
}

func TestWriteWrappers_EmptyHomeReturnsNothing(t *testing.T) {
	installed, err := WriteWrappers("")
	if err != nil {
		t.Fatalf("WriteWrappers on empty HOME: %v", err)
	}
	if installed != nil {
		t.Fatalf("expected nil installed list on empty HOME, got %v", installed)
	}
}

// TestSetupHarnesses_UnsupportedShortCircuits guards the contract that
// invalid harness names produce a result entry with ExitCode != 0 +
// non-nil Err without invoking the okt binary (which the test fixture
// doesn't even need to provide).
func TestSetupHarnesses_UnsupportedShortCircuits(t *testing.T) {
	results := SetupHarnesses(context.Background(), "/nonexistent/okt", []string{"bogus"})
	if len(results) != 1 {
		t.Fatalf("results: got %d want 1", len(results))
	}
	if results[0].Status != "unsupported" {
		t.Fatalf("status: got %q want %q", results[0].Status, "unsupported")
	}
	if results[0].Err == nil {
		t.Fatalf("expected non-nil Err for unsupported harness")
	}
	if results[0].ExitCode == 0 {
		t.Fatalf("expected non-zero ExitCode for unsupported harness")
	}
}

func TestSetupHarnesses_EmptyNamesIsNoOp(t *testing.T) {
	results := SetupHarnesses(context.Background(), "/nonexistent/okt", nil)
	if len(results) != 0 {
		t.Fatalf("expected empty result slice, got %v", results)
	}
}

// TestPowerShellProfileTargets_PerOS pins the per-OS resolution that
// WritePowerShellWrappers depends on. The PS wrapper is Windows-only
// (the wrapper body shells into `okt.exe`, the Windows-only binary
// name); non-Windows hosts return nil so the bash wrapper stays the
// canonical surface there.
func TestPowerShellProfileTargets_PerOS(t *testing.T) {
	got := PowerShellProfileTargets("")
	if got != nil {
		t.Fatalf("empty home should return nil targets, got %v", got)
	}

	got = PowerShellProfileTargets("/tmp/h")
	if runtime.GOOS == "windows" {
		want := []string{
			filepath.Join("/tmp/h", "Documents", "PowerShell", "profile.ps1"),
			filepath.Join("/tmp/h", "Documents", "WindowsPowerShell", "profile.ps1"),
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("windows targets mismatch (-want +got):\n%s", diff)
		}
		return
	}
	if got != nil {
		t.Fatalf("non-windows targets must be nil, got %v", got)
	}
}

// TestWritePowerShellWrappers_NoopOnPosix asserts the non-Windows
// behaviour: the writer is a no-op and never materialises a PS profile
// on hosts where the wrapper body (`& okt.exe @args`) would not work.
func TestWritePowerShellWrappers_NoopOnPosix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only path; the Windows test covers the force-create + touch-if-exists shape")
	}
	home := t.TempDir()
	installed, err := WritePowerShellWrappers(home)
	if err != nil {
		t.Fatalf("WritePowerShellWrappers: %v", err)
	}
	if installed != nil {
		t.Fatalf("expected nil installed list on posix host, got %v", installed)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "powershell", "profile.ps1")); err == nil {
		t.Fatalf("WritePowerShellWrappers materialised profile.ps1 on a posix host")
	}
}

// TestWritePowerShellWrappers_EmptyHomeReturnsNothing mirrors the
// matching bash-side test — an empty HOME must produce a no-op result
// without erroring so callers can safely thread `os.UserHomeDir()`'s
// nil-on-error path through.
func TestWritePowerShellWrappers_EmptyHomeReturnsNothing(t *testing.T) {
	installed, err := WritePowerShellWrappers("")
	if err != nil {
		t.Fatalf("WritePowerShellWrappers on empty HOME: %v", err)
	}
	if installed != nil {
		t.Fatalf("expected nil installed list on empty HOME, got %v", installed)
	}
}

// TestSetupHarnesses_RecordsExecFailure runs the helper against a fake
// "okt" binary the test creates as a script that always exits 7. The
// exit code must propagate into the result so the caller can render
// the `cli.setup.status.harness_failed` line with the same %d the
// bash version embedded.
func TestSetupHarnesses_RecordsExecFailure(t *testing.T) {
	tmp := t.TempDir()
	stub := filepath.Join(tmp, "okt")
	const script = "#!/bin/sh\nexit 7\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	results := SetupHarnesses(context.Background(), stub, []string{"claude-code"})
	if len(results) != 1 {
		t.Fatalf("results: got %d want 1", len(results))
	}
	if results[0].Status != "failed" {
		t.Fatalf("status: got %q want %q", results[0].Status, "failed")
	}
	if results[0].ExitCode != 7 {
		t.Fatalf("exit code: got %d want 7", results[0].ExitCode)
	}
}
