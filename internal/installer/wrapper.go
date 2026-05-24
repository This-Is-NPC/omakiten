package installer

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WrapperBegin / WrapperEnd delimit the okt() shell-wrapper block in
// user rc files. The strings must stay byte-identical with the values
// hardcoded at install.sh:10-11 and uninstall.sh:8-9 — the
// scripts/wrapper_idempotency_test.sh fixture asserts the bash and Go
// writers agree, and the uninstaller drops lines between exactly these
// sentinels.
const (
	WrapperBegin = "# >>> okt wrapper >>>"
	WrapperEnd   = "# <<< okt wrapper <<<"
)

// wrapperBody is the literal okt() shell function written between the
// sentinels for bash/zsh rc files. Both bash and PowerShell wrappers
// only ever land via the Go installer now — scripts/wrapper_idempotency_test.sh
// and scripts/wrapper_idempotency_test.ps1 build the binary and re-run
// `okt setup` against a seeded rc/profile to confirm a second install
// lands the same bytes.
const wrapperBody = `# Auto-installed by omakiten. Lets ` + "`okt tui`" + ` cd the parent shell
# into the project chosen on the Home screen when the TUI exits.
# Remove with the bundled uninstall.sh; do not edit by hand.
okt() {
  local cd_file="${OKT_CD_FILE:-${XDG_RUNTIME_DIR:-${TMPDIR:-/tmp}}/okt-cd}"
  if [ -z "${XDG_RUNTIME_DIR:-}" ] && [ -z "${OKT_CD_FILE:-}" ]; then
    cd_file="${TMPDIR:-/tmp}/okt-cd-$(id -u)"
  fi
  rm -f "$cd_file" 2>/dev/null || true
  command okt "$@"
  local rc=$?
  if [ -f "$cd_file" ]; then
    local target
    target="$(head -n 1 "$cd_file")"
    rm -f "$cd_file" 2>/dev/null || true
    if [ -n "$target" ] && [ -d "$target" ]; then
      cd "$target" || return $rc
    fi
  fi
  return $rc
}`

// powerShellWrapperBody is the PowerShell counterpart of wrapperBody.
// Sentinels stay byte-identical so a single uninstaller regex strips
// either flavour; the function calls `okt.exe` explicitly to bypass the
// wrapping function (PowerShell's `command` builtin is the bash-only
// idiom, so an explicit `.exe` is the portable way to disambiguate
// here).
const powerShellWrapperBody = `# Auto-installed by omakiten. Lets ` + "`okt tui`" + ` cd the parent shell
# into the project chosen on the Home screen when the TUI exits.
# Remove with the bundled uninstall.ps1; do not edit by hand.
function okt {
    $cdFile = if ($env:OKT_CD_FILE) { $env:OKT_CD_FILE } elseif ($env:XDG_RUNTIME_DIR) { Join-Path $env:XDG_RUNTIME_DIR 'okt-cd' } else { Join-Path $env:TEMP 'okt-cd' }
    if (Test-Path $cdFile) { Remove-Item $cdFile -Force -ErrorAction SilentlyContinue }
    & okt.exe @args
    $rc = $LASTEXITCODE
    if (Test-Path $cdFile) {
        $target = (Get-Content $cdFile -TotalCount 1).Trim()
        Remove-Item $cdFile -Force -ErrorAction SilentlyContinue
        if ($target -and (Test-Path $target -PathType Container)) { Set-Location $target }
    }
    return $rc
}`

// WrapperBlock returns the full sentinel-wrapped bash/zsh block (without
// a trailing newline) the installer writes into rc files. Exported so
// callers can preview the bytes for a `--dry-run` mode or for tests
// that need to assert on the wrapper contents without filesystem
// involvement.
func WrapperBlock() string {
	return WrapperBegin + "\n" + wrapperBody + "\n" + WrapperEnd
}

// PowerShellWrapperBlock returns the sentinel-wrapped PowerShell block
// the installer writes into `$PROFILE.CurrentUserAllHosts`. The block
// shares its sentinels with WrapperBlock so the uninstaller scrubs
// either flavour with the same regex.
func PowerShellWrapperBlock() string {
	return WrapperBegin + "\n" + powerShellWrapperBody + "\n" + WrapperEnd
}

// InstallWrapper writes (or replaces) the bash/zsh wrapper block in
// rcPath. Behaviour mirrors install.sh's install_wrapper_into; see
// installBlockInto for the shared swap-or-append semantics.
func InstallWrapper(rcPath string) error {
	return installBlockInto(rcPath, WrapperBlock())
}

// InstallPowerShellWrapper writes (or replaces) the PowerShell wrapper
// block in profilePath (a PS host AllHosts profile — typically one of
// the paths PowerShellProfileTargets returns: `Documents\PowerShell\
// profile.ps1` for PS 7 or `Documents\WindowsPowerShell\profile.ps1`
// for PS 5.1). It shares the swap-in-place / append-with-separator
// behaviour of InstallWrapper so the on-disk shape stays consistent
// across flavours and a single uninstaller pattern keeps working.
func InstallPowerShellWrapper(profilePath string) error {
	return installBlockInto(profilePath, PowerShellWrapperBlock())
}

// installBlockInto is the shared writer used by InstallWrapper and
// InstallPowerShellWrapper:
//   - if the file does not exist, it is created (parent dirs included)
//     with the block prefixed by a single blank-line separator (so a
//     freshly-created file starts with one leading "\n");
//   - if the file lacks the sentinel, append the block prefixed by a
//     blank line (matching the bash `printf '\n%s\n' "$block" >> "$rc"`
//     idiom);
//   - if the file already contains the sentinel, swap the existing
//     block in place — every line between WrapperBegin and the matching
//     WrapperEnd is replaced by the new block, surrounding content
//     stays byte-identical.
//
// The function is idempotent against itself: re-running with the same
// block produces the same bytes, which is the invariant the
// scripts/wrapper_idempotency_test.{sh,ps1} fixtures exercise.
func installBlockInto(rcPath, block string) error {
	if rcPath == "" {
		return fmt.Errorf("installer: empty rc path")
	}
	existing, err := os.ReadFile(rcPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read %s: %w", rcPath, err)
		}
		// 0700 — the rc file lives under $HOME, contains shell wiring
		// the installer wrote on the user's behalf, and should not be
		// readable by other accounts on a shared box. The narrower
		// mode also covers `.profile`-adjacent files that hold per-
		// user PATH overrides.
		if err := os.MkdirAll(filepath.Dir(rcPath), 0o700); err != nil {
			return fmt.Errorf("mkdir for %s: %w", rcPath, err)
		}
		existing = nil
	}

	updated, replaced := replaceBlock(existing, block)
	if !replaced {
		var buf bytes.Buffer
		buf.Write(existing)
		buf.WriteByte('\n')
		buf.WriteString(block)
		buf.WriteByte('\n')
		updated = buf.Bytes()
	}

	// 0600 — same rationale as the parent directory: shell wiring +
	// user-specific PATH overrides should not be world-readable.
	if err := os.WriteFile(rcPath, updated, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", rcPath, err)
	}
	return nil
}

// RemoveWrapper strips the wrapper block from rcPath, leaving
// surrounding content untouched. A missing file or a file without the
// sentinel is a no-op (returns nil) — mirroring uninstall.sh's
// remove_from which exits 0 in both cases.
func RemoveWrapper(rcPath string) (removed bool, err error) {
	existing, err := os.ReadFile(rcPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read %s: %w", rcPath, err)
	}
	if !bytes.Contains(existing, []byte(WrapperBegin)) {
		return false, nil
	}
	updated := stripBlock(existing)
	if bytes.Equal(updated, existing) {
		return false, nil
	}
	// Match WriteWrapper's 0o600 — strip-and-rewrite should not
	// silently widen permissions on a file the installer originally
	// wrote at 0o600.
	if err := os.WriteFile(rcPath, updated, 0o600); err != nil {
		return false, fmt.Errorf("write %s: %w", rcPath, err)
	}
	return true, nil
}

// replaceBlock swaps the first WrapperBegin..WrapperEnd region in src
// with block (verbatim, no surrounding newlines added — the caller
// preserves the line that precedes the original WrapperBegin). Returns
// the rewritten bytes and a boolean indicating whether a swap actually
// happened.
//
// Matches the awk pattern in install.sh:
//
//	$0 == begin { skipping = 1; print block; next }
//	skipping && $0 == end { skipping = 0; next }
//	!skipping
//
// i.e. line-anchored equality against the sentinels (a stray
// "echo \"# >>> okt wrapper >>>\"" embedded in a longer line stays
// verbatim because we only match whole lines).
func replaceBlock(src []byte, block string) ([]byte, bool) {
	if len(src) == 0 {
		return src, false
	}
	lines := splitLinesPreserveTrailing(src)
	var out bytes.Buffer
	out.Grow(len(src) + len(block))
	skipping := false
	swapped := false
	for _, line := range lines {
		trimmed := trimNewline(line)
		switch {
		case !skipping && trimmed == WrapperBegin:
			skipping = true
			out.WriteString(block)
			// Preserve whichever line terminator the original
			// WrapperBegin carried so swapping inside a CRLF file
			// stays CRLF-clean.
			out.WriteString(lineTerminator(line))
			swapped = true
		case skipping && trimmed == WrapperEnd:
			skipping = false
		case !skipping:
			out.WriteString(line)
		}
	}
	if !swapped {
		return src, false
	}
	return out.Bytes(), true
}

// stripBlock removes every WrapperBegin..WrapperEnd region from src,
// leaving the rest byte-identical. Mirrors uninstall.sh's awk filter.
func stripBlock(src []byte) []byte {
	lines := splitLinesPreserveTrailing(src)
	var out bytes.Buffer
	out.Grow(len(src))
	skipping := false
	for _, line := range lines {
		trimmed := trimNewline(line)
		switch {
		case !skipping && trimmed == WrapperBegin:
			skipping = true
		case skipping && trimmed == WrapperEnd:
			skipping = false
		case !skipping:
			out.WriteString(line)
		}
	}
	return out.Bytes()
}

func splitLinesPreserveTrailing(src []byte) []string {
	s := string(src)
	if s == "" {
		return nil
	}
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i+1])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimNewline(line string) string {
	return strings.TrimRight(line, "\r\n")
}

func lineTerminator(line string) string {
	if strings.HasSuffix(line, "\r\n") {
		return "\r\n"
	}
	if strings.HasSuffix(line, "\n") {
		return "\n"
	}
	return ""
}
