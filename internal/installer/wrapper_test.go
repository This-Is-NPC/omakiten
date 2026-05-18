package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWrapperBlockSentinels asserts the exact byte-for-byte sentinel
// strings the shell scripts depend on. Drift in either direction
// breaks scripts/wrapper_idempotency_test.sh (which extracts the bash
// install_wrapper_into and replays it against a fresh tmp rc) and the
// shipped uninstall.sh (which greps for WrapperBegin to decide whether
// to scrub the block).
func TestWrapperBlockSentinels(t *testing.T) {
	if WrapperBegin != "# >>> okt wrapper >>>" {
		t.Fatalf("WrapperBegin drifted: got %q", WrapperBegin)
	}
	if WrapperEnd != "# <<< okt wrapper <<<" {
		t.Fatalf("WrapperEnd drifted: got %q", WrapperEnd)
	}
	block := WrapperBlock()
	if !strings.HasPrefix(block, WrapperBegin+"\n") {
		t.Fatalf("WrapperBlock must start with the begin sentinel + LF; got %q", block[:len(WrapperBegin)+8])
	}
	if !strings.HasSuffix(block, "\n"+WrapperEnd) {
		t.Fatalf("WrapperBlock must end with LF + the end sentinel; got tail %q", block[len(block)-len(WrapperEnd)-1:])
	}
}

func TestInstallWrapper_CreatesFile(t *testing.T) {
	rc := filepath.Join(t.TempDir(), ".bashrc")
	if err := InstallWrapper(rc); err != nil {
		t.Fatalf("InstallWrapper on missing file: %v", err)
	}
	got, err := os.ReadFile(rc)
	if err != nil {
		t.Fatalf("read rc: %v", err)
	}
	if !strings.Contains(string(got), WrapperBegin) {
		t.Fatalf("file missing begin sentinel: %s", got)
	}
	if !strings.Contains(string(got), WrapperEnd) {
		t.Fatalf("file missing end sentinel: %s", got)
	}
	if strings.Count(string(got), WrapperBegin) != 1 {
		t.Fatalf("expected one begin sentinel, got %d", strings.Count(string(got), WrapperBegin))
	}
}

// TestInstallWrapper_Idempotent mirrors the assertion in
// scripts/wrapper_idempotency_test.sh: re-running install on the same
// file must not duplicate the block.
func TestInstallWrapper_Idempotent(t *testing.T) {
	rc := filepath.Join(t.TempDir(), ".bashrc")
	const seed = "# user content above\nexport FOO=bar\n"
	if err := os.WriteFile(rc, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed rc: %v", err)
	}

	if err := InstallWrapper(rc); err != nil {
		t.Fatalf("first install: %v", err)
	}
	first, err := os.ReadFile(rc)
	if err != nil {
		t.Fatalf("read after first install: %v", err)
	}
	if got := strings.Count(string(first), WrapperBegin); got != 1 {
		t.Fatalf("first install: want 1 begin sentinel, got %d", got)
	}
	if !strings.Contains(string(first), "export FOO=bar") {
		t.Fatalf("first install dropped the seed content: %s", first)
	}

	if err := InstallWrapper(rc); err != nil {
		t.Fatalf("second install: %v", err)
	}
	second, err := os.ReadFile(rc)
	if err != nil {
		t.Fatalf("read after second install: %v", err)
	}
	if got := strings.Count(string(second), WrapperBegin); got != 1 {
		t.Fatalf("re-install duplicated the block: got %d sentinels", got)
	}
	if string(first) != string(second) {
		t.Fatalf("idempotent install diverged:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

// TestRemoveWrapper_Surgical mirrors the parallel uninstall assertion:
// the wrapper block disappears while every other line in the rc file
// stays byte-for-byte intact.
func TestRemoveWrapper_Surgical(t *testing.T) {
	rc := filepath.Join(t.TempDir(), ".bashrc")
	const seed = "# user content above\nexport FOO=bar\n"
	if err := os.WriteFile(rc, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed rc: %v", err)
	}
	if err := InstallWrapper(rc); err != nil {
		t.Fatalf("install: %v", err)
	}
	removed, err := RemoveWrapper(rc)
	if err != nil {
		t.Fatalf("RemoveWrapper: %v", err)
	}
	if !removed {
		t.Fatalf("RemoveWrapper reported nothing removed but the block was present")
	}
	got, err := os.ReadFile(rc)
	if err != nil {
		t.Fatalf("read after remove: %v", err)
	}
	if strings.Contains(string(got), WrapperBegin) {
		t.Fatalf("remove left a begin sentinel behind: %s", got)
	}
	if !strings.Contains(string(got), "export FOO=bar") {
		t.Fatalf("remove dropped unrelated content: %s", got)
	}
	if !strings.Contains(string(got), "# user content above") {
		t.Fatalf("remove dropped the leading comment: %s", got)
	}

	// Re-running uninstall is a no-op: no sentinel, no change.
	removed, err = RemoveWrapper(rc)
	if err != nil {
		t.Fatalf("second RemoveWrapper: %v", err)
	}
	if removed {
		t.Fatalf("second RemoveWrapper reported removal on a clean file")
	}
}

func TestRemoveWrapper_MissingFile(t *testing.T) {
	rc := filepath.Join(t.TempDir(), "does-not-exist")
	removed, err := RemoveWrapper(rc)
	if err != nil {
		t.Fatalf("RemoveWrapper on missing file should be a no-op, got: %v", err)
	}
	if removed {
		t.Fatalf("RemoveWrapper reported removal on a missing file")
	}
}

// TestPowerShellWrapperBlockSentinels mirrors TestWrapperBlockSentinels
// for the PS flavour: sentinels stay byte-identical with the bash
// block so a single uninstaller regex strips either rc shape, and the
// block opens with the begin sentinel + LF / closes with LF + end so
// install.ps1's prior Add-Content / Set-Content shape is preserved.
func TestPowerShellWrapperBlockSentinels(t *testing.T) {
	block := PowerShellWrapperBlock()
	if !strings.HasPrefix(block, WrapperBegin+"\n") {
		t.Fatalf("PowerShellWrapperBlock must start with begin sentinel + LF; got %q", block[:len(WrapperBegin)+8])
	}
	if !strings.HasSuffix(block, "\n"+WrapperEnd) {
		t.Fatalf("PowerShellWrapperBlock must end with LF + end sentinel; got tail %q", block[len(block)-len(WrapperEnd)-1:])
	}
	if !strings.Contains(block, "function okt {") {
		t.Fatalf("PowerShellWrapperBlock missing function declaration: %s", block)
	}
	// Bash function form must not appear in the PS block — they are
	// rendered into the same sentinel range but never both at once.
	if strings.Contains(block, "okt() {") {
		t.Fatalf("PowerShellWrapperBlock leaked bash function form: %s", block)
	}
}

// TestInstallPowerShellWrapper_Idempotent re-runs the install against a
// seeded profile and asserts the swap produced a single sentinel
// region (no duplicate blocks) and that surrounding content survives.
// Mirror of TestInstallWrapper_Idempotent for the PS path.
func TestInstallPowerShellWrapper_Idempotent(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "profile.ps1")
	const seed = "# user content above\n$env:FOO = 'bar'\n"
	if err := os.WriteFile(profile, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed profile: %v", err)
	}

	if err := InstallPowerShellWrapper(profile); err != nil {
		t.Fatalf("first install: %v", err)
	}
	first, err := os.ReadFile(profile)
	if err != nil {
		t.Fatalf("read after first install: %v", err)
	}
	if got := strings.Count(string(first), WrapperBegin); got != 1 {
		t.Fatalf("first install: want 1 begin sentinel, got %d", got)
	}
	if !strings.Contains(string(first), "$env:FOO = 'bar'") {
		t.Fatalf("first install dropped seeded content: %s", first)
	}
	if !strings.Contains(string(first), "function okt {") {
		t.Fatalf("first install missing PS function: %s", first)
	}

	if err := InstallPowerShellWrapper(profile); err != nil {
		t.Fatalf("second install: %v", err)
	}
	second, err := os.ReadFile(profile)
	if err != nil {
		t.Fatalf("read after second install: %v", err)
	}
	if got := strings.Count(string(second), WrapperBegin); got != 1 {
		t.Fatalf("re-install duplicated the block: got %d sentinels", got)
	}
	if string(first) != string(second) {
		t.Fatalf("idempotent install diverged:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

// TestInstallPowerShellWrapper_CreatesMissingProfile pins
// install.ps1's `New-Item -ItemType File -Force` behaviour for the
// fresh-Windows-install case where `$PROFILE.CurrentUserAllHosts` does
// not exist yet. The Go writer must materialise the file (and any
// missing parent dirs) on demand.
func TestInstallPowerShellWrapper_CreatesMissingProfile(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "Documents", "PowerShell", "profile.ps1")
	if err := InstallPowerShellWrapper(profile); err != nil {
		t.Fatalf("InstallPowerShellWrapper on missing path: %v", err)
	}
	got, err := os.ReadFile(profile)
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}
	if !strings.Contains(string(got), WrapperBegin) {
		t.Fatalf("created profile missing begin sentinel: %s", got)
	}
	if strings.Count(string(got), WrapperBegin) != 1 {
		t.Fatalf("expected one begin sentinel, got %d", strings.Count(string(got), WrapperBegin))
	}
}

// TestInstallWrapper_PreservesCRLF guards the rare case where a user
// edited their rc file on Windows and the sentinel line carries CRLF
// rather than LF. The swap path must preserve whichever terminator
// already lived on the WrapperBegin line so the file stays
// internally consistent.
func TestInstallWrapper_PreservesCRLF(t *testing.T) {
	rc := filepath.Join(t.TempDir(), ".bashrc")
	seed := "first\r\n" + WrapperBegin + "\r\nold body\r\n" + WrapperEnd + "\r\nlast\r\n"
	if err := os.WriteFile(rc, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := InstallWrapper(rc); err != nil {
		t.Fatalf("install: %v", err)
	}
	got, err := os.ReadFile(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.HasPrefix(string(got), "first\r\n") {
		t.Fatalf("CRLF prefix lost: %q", got)
	}
	if !strings.HasSuffix(string(got), "last\r\n") {
		t.Fatalf("CRLF suffix lost: %q", got)
	}
	if strings.Count(string(got), WrapperBegin) != 1 {
		t.Fatalf("expected one begin sentinel after CRLF swap, got %d", strings.Count(string(got), WrapperBegin))
	}
}
