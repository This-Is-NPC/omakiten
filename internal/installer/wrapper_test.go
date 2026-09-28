package installer

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestWrapperBlockSentinels pins the bootstrap uninstallers' delimiters.
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

// TestInstallWrapper_Idempotent rejects duplicate wrapper blocks.
func TestInstallWrapper_Idempotent(t *testing.T) {
	rc := filepath.Join(t.TempDir(), ".bashrc")
	const seed = "# user content above\nexport FOO=bar\n"
	if err := os.WriteFile(rc, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	assertWrapperInstallIdempotent(t, rc, InstallWrapper)
	got, err := os.ReadFile(rc)
	if err != nil || !strings.HasPrefix(string(got), seed) {
		t.Fatalf("install changed the seed content: %v\n%s", err, got)
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
		t.Fatal(err)
	}
	assertWrapperInstallIdempotent(t, profile, InstallPowerShellWrapper)
	got, err := os.ReadFile(profile)
	if err != nil || !strings.HasPrefix(string(got), seed) || !strings.Contains(string(got), "function okt {") {
		t.Fatalf("install changed the seed content or omitted the PowerShell function: %v\n%s", err, got)
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

type wrapperRoundTripCase struct {
	tool, file, profile, seed string
	install                   func(string) error
}

func TestWrapperBootstrapUninstallRoundTrip(t *testing.T) {
	cases := map[string]wrapperRoundTripCase{
		"bash":            {"bash", "uninstall.sh", ".bashrc", "# user content\nexport FOO=bar\n", InstallWrapper},
		"powershell":      {"pwsh", "uninstall.ps1", filepath.Join("Documents", "PowerShell", "profile.ps1"), "# user content\n$env:FOO = 'bar'\n", InstallPowerShellWrapper},
		"bash CRLF":       {"bash", "uninstall.sh", ".bashrc", "# user content\r\nexport FOO=bar\r\n", InstallWrapper},
		"powershell CRLF": {"pwsh", "uninstall.ps1", filepath.Join("Documents", "PowerShell", "profile.ps1"), "# user content\r\n$env:FOO = 'bar'\r\n", InstallPowerShellWrapper},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) { runWrapperRoundTrip(t, tc) })
	}
}

func runWrapperRoundTrip(t *testing.T, tc wrapperRoundTripCase) {
	t.Helper()
	tool, err := exec.LookPath(tc.tool)
	if err != nil {
		t.Skipf("%s is not installed", tc.tool)
	}
	root := t.TempDir()
	profile := filepath.Join(root, tc.profile)
	if err := os.MkdirAll(filepath.Dir(profile), 0o700); err != nil {
		t.Fatal(err)
	}
	newline := "\n"
	if strings.Contains(tc.seed, "\r\n") {
		newline = "\r\n"
	}
	seed := tc.seed + newline
	if err := os.WriteFile(profile, []byte(seed+WrapperBegin+newline+"stale wrapper"+newline+WrapperEnd+newline), 0o600); err != nil {
		t.Fatal(err)
	}
	assertWrapperInstallIdempotent(t, profile, tc.install)
	script, err := filepath.Abs(filepath.Join("..", "..", tc.file))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		runBootstrapUninstaller(t, tool, tc.tool, script, root)
		got, err := os.ReadFile(profile)
		if err != nil || string(got) != seed {
			t.Fatalf("uninstaller changed unrelated bytes: %v\n%q, want %q", err, got, seed)
		}
	}
}

func assertWrapperInstallIdempotent(t *testing.T, profile string, install func(string) error) {
	t.Helper()
	var first []byte
	for attempt := range 2 {
		if err := install(profile); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(profile)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(got), WrapperBegin) != 1 {
			t.Fatalf("expected one wrapper: %s", got)
		}
		if attempt == 1 && string(first) != string(got) {
			t.Fatal("repeated install changed the wrapper")
		}
		first = got
	}
}

func runBootstrapUninstaller(t *testing.T, tool, name, script, root string) {
	t.Helper()
	args := []string{script}
	if name == "pwsh" {
		args = []string{"-NoProfile", "-NonInteractive", "-File", script}
	}
	cmd := exec.Command(tool, args...)
	cmd.Env = append(os.Environ(), "HOME="+root, "USERPROFILE="+root, "INSTALL_DIR="+filepath.Join(root, "bin"), "LOCALAPPDATA="+filepath.Join(root, "local"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bootstrap uninstaller: %v\n%s", err, out)
	}
}
