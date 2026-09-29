package installer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// WrapperTargets is the set of rc files the installer considers writing
// the okt() wrapper into. Order is deterministic so test assertions
// against the "installed into" log line stay stable. The bash installer
// touches .bashrc + .zshrc only when they already exist OR when the
// invoking shell matches; WriteWrappers takes the simpler stance
// (touch only when the file exists) because the Go path runs from the
// post-binary-install context where SHELL detection is unreliable
// (curl|bash already changed the parent shell).
//
// Each entry is an absolute path. Callers pass HOME explicitly so tests
// can pin a tmpdir without env-var stomping.
func WrapperTargets(home string) []string {
	if home == "" {
		return nil
	}
	return []string{
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".zshrc"),
	}
}

// WriteWrappers calls InstallWrapper for every rc file in
// WrapperTargets(home) that exists on disk. Returns the slice of rc
// paths the wrapper landed in so the caller can render the
// `cli.setup.status.wrapper_written` line with the same list bash
// echoed via `installed_into[*]`.
//
// Skipped rc files (missing) are not an error — the installer is fine
// with a user who only runs one shell. When the slice is empty the
// caller prints `cli.setup.status.wrapper_skipped` instead.
func WriteWrappers(home string) ([]string, error) {
	var installed []string
	for _, rc := range WrapperTargets(home) {
		if _, err := os.Stat(rc); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("stat %s: %w", rc, err)
		}
		if err := InstallWrapper(rc); err != nil {
			return nil, err
		}
		installed = append(installed, rc)
	}
	return installed, nil
}

// PowerShellProfileTargets is the ordered set of PowerShell AllHosts
// profile paths the installer considers for the PowerShell-flavoured
// wrapper. Windows-only because the wrapper body shells into `okt.exe`,
// the binary name only Windows ships; PS-on-Linux users are served by
// the bash wrapper. Order matches modern → legacy: PS 7
// (`Documents\PowerShell\profile.ps1`) first, PS 5.1
// (`Documents\WindowsPowerShell\profile.ps1`) second.
//
// Note: `$PROFILE.CurrentUserAllHosts` is the runtime variable for the
// CURRENTLY hosting PS version only; this function returns both PS host
// AllHosts paths so a single `okt setup` invocation covers a box where
// both PS 7 and PS 5.1 are in use. uninstall.ps1 mirrors the same pair
// (see Remove-OktWrapperFrom) so the install/uninstall surfaces stay
// symmetric.
//
// install.ps1 historically wrote the wrapper via PowerShell itself
// against `$PROFILE.CurrentUserAllHosts`; porting that here lets the
// Go installer own both flavours and removes the duplicate sentinel /
// wrapper-body literal that lived in install.ps1.
func PowerShellProfileTargets(home string) []string {
	if home == "" || runtime.GOOS != "windows" {
		return nil
	}
	return []string{
		filepath.Join(home, "Documents", "PowerShell", "profile.ps1"),
		filepath.Join(home, "Documents", "WindowsPowerShell", "profile.ps1"),
	}
}

// WritePowerShellWrappers installs the PS-flavoured okt() function
// into the user's PowerShell profile(s) on Windows. The canonical PS 7
// profile (first entry of PowerShellProfileTargets) is always written —
// parent directories are created on demand to match install.ps1's
// `New-Item -ItemType File -Force` behaviour against
// `$PROFILE.CurrentUserAllHosts`. The legacy PS 5.1 profile is touched
// only when it already exists so we do not materialise an unexpected
// `WindowsPowerShell\` folder for a user who only runs PS 7.
//
// Non-Windows hosts get nothing — PowerShellProfileTargets returns nil
// and the function is a no-op.
//
// Returns the absolute profile paths the wrapper landed in so the
// caller can echo `installed_into[*]` alongside the bash result.
func WritePowerShellWrappers(home string) ([]string, error) {
	targets := PowerShellProfileTargets(home)
	if len(targets) == 0 {
		return nil, nil
	}
	var installed []string
	for i, p := range targets {
		// First target is the canonical fresh-install path —
		// install.ps1 used to create the profile file outright on a
		// fresh box; preserve that contract so curl|iwr installs land a
		// working wrapper without a second manual step.
		if i > 0 {
			if _, err := os.Stat(p); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return nil, fmt.Errorf("stat %s: %w", p, err)
			}
		}
		if err := InstallPowerShellWrapper(p); err != nil {
			return nil, err
		}
		installed = append(installed, p)
	}
	return installed, nil
}
