// Package lifecycle owns the in-binary primitives `okt uninstall` and
// `okt update` rely on: binary path resolution, wrapper scrubbing across
// every rc/profile target the installer wrote into, and Go-native purge
// of the data + config directories.
//
// The helpers are factored from install.sh/uninstall.sh + install.ps1/
// uninstall.ps1 so the legacy curl|bash bootstrap path and the new
// in-binary commands stay byte-for-byte symmetric. No shell-out — the
// test suite can exercise every primitive without a host shell.
package lifecycle

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"omakiten/internal/installer"
	"omakiten/internal/paths"
)

// InstallDirEnv is the env var the bash/PowerShell installers honour to
// override the default install directory. Resolved by BinaryPath so the
// in-binary uninstall finds the same binary the installer wrote.
const InstallDirEnv = "INSTALL_DIR"

// BinaryName returns the basename of the okt binary for the current OS.
// Windows ships okt.exe; everywhere else ships okt.
func BinaryName() string {
	if runtime.GOOS == "windows" {
		return "okt.exe"
	}
	return "okt"
}

// DefaultInstallDir mirrors the install.sh / install.ps1 default install
// location. Used when $INSTALL_DIR is unset.
func DefaultInstallDir(home string) string {
	if home == "" {
		return ""
	}
	if runtime.GOOS == "windows" {
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			local = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(local, "Programs", "okt")
	}
	return filepath.Join(home, ".local", "bin")
}

// BinaryPath returns the absolute path of the installed binary the
// uninstall + update commands should target. Resolution order matches
// the installer scripts:
//
//  1. $INSTALL_DIR/<binary>
//  2. DefaultInstallDir(home)/<binary>
//
// home must be the user's $HOME. The caller (`os.UserHomeDir`) handles
// the system-side resolution.
func BinaryPath(home string) string {
	dir := os.Getenv(InstallDirEnv)
	if dir == "" {
		dir = DefaultInstallDir(home)
	}
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, BinaryName())
}

// RemoveBinary deletes path. A missing file is a no-op (returns false,
// nil) so the caller can render a `binary_not_found` status without
// special-casing os.IsNotExist at every call site.
func RemoveBinary(path string) (removed bool, err error) {
	if path == "" {
		return false, nil
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("stat %s: %w", path, err)
	}
	if err := os.Remove(path); err != nil {
		return false, fmt.Errorf("remove %s: %w", path, err)
	}
	return true, nil
}

// WrapperTargetsAll returns the union of every rc/profile path the
// installer might have written the okt() wrapper into. Mirrors
// install.sh + install.ps1 so the uninstall surface scrubs every
// flavour without leaking blocks into shells the user no longer runs.
//
// home is the user's $HOME; an empty value returns nil.
func WrapperTargetsAll(home string) []string {
	out := installer.WrapperTargets(home)
	out = append(out, installer.PowerShellProfileTargets(home)...)
	return out
}

// RemoveAllWrappers walks WrapperTargetsAll(home), calling
// installer.RemoveWrapper for each existing file. Returns the slice of
// rc/profile paths the wrapper actually came out of so the caller can
// render the same `installed_into` line bash echoed.
//
// Per-file errors abort the loop — a wrapper-removal failure usually
// means the rc file is read-only or the user lost permissions, both
// states that warrant surfacing rather than silently continuing.
func RemoveAllWrappers(home string) ([]string, error) {
	var removed []string
	for _, rc := range WrapperTargetsAll(home) {
		ok, err := installer.RemoveWrapper(rc)
		if err != nil {
			return removed, err
		}
		if ok {
			removed = append(removed, rc)
		}
	}
	return removed, nil
}

// PreviewDataDir returns the data directory path + whether it exists
// on disk without modifying anything. The picker uses this pair to
// render the size line ("data dir  /home/u/.local/share/omakiten  (8.2 MiB)")
// before the user toggles the purge checkbox.
func PreviewDataDir() (path string, exists bool, err error) {
	dir, err := paths.DataDir()
	if err != nil {
		return "", false, err
	}
	return dir, dirExists(dir), nil
}

// PreviewConfigRoot is the config-root counterpart of PreviewDataDir.
func PreviewConfigRoot() (path string, exists bool, err error) {
	dir, err := paths.ConfigRoot()
	if err != nil {
		return "", false, err
	}
	return dir, dirExists(dir), nil
}

func dirExists(dir string) bool {
	if dir == "" {
		return false
	}
	info, err := os.Stat(dir)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// PurgeDataDir removes the entire data directory (paths.DataDir() —
// `$XDG_DATA_HOME/omakiten` or `~/.local/share/omakiten` by default).
// Returns the path scrubbed and whether anything existed.
//
// Irreversible: deletes the SQLite database + WAL/SHM files alongside
// any debug artefacts the runtime dropped under the same root.
func PurgeDataDir() (path string, removed bool, err error) {
	dir, err := paths.DataDir()
	if err != nil {
		return "", false, err
	}
	return purgeDir(dir)
}

// PurgeConfigRoot removes the entire config root (paths.ConfigRoot() —
// `$XDG_CONFIG_HOME/omakiten` or `~/.config/omakiten` by default).
// Returns the path scrubbed and whether anything existed.
//
// Irreversible: deletes the active yaml profile, every entity folder
// (personas/, laws/, skills/, templates/, themes/, languages/), and
// the user's `custom/` overrides.
func PurgeConfigRoot() (path string, removed bool, err error) {
	dir, err := paths.ConfigRoot()
	if err != nil {
		return "", false, err
	}
	return purgeDir(dir)
}

func purgeDir(dir string) (string, bool, error) {
	if dir == "" {
		return "", false, nil
	}
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return dir, false, nil
		}
		return dir, false, fmt.Errorf("stat %s: %w", dir, err)
	}
	if err := os.RemoveAll(dir); err != nil {
		return dir, false, fmt.Errorf("remove %s: %w", dir, err)
	}
	return dir, true, nil
}

// DirSize sums the on-disk size of every regular file under root,
// recursively. Symlinks contribute their own size (no following). A
// missing root returns 0 with no error so the picker can render
// "0 B" for absent purge targets without special-casing IsNotExist.
//
// Returned bytes are int64 to match os.FileInfo.Size(); the picker
// formatter (FormatBytes) accepts the same type.
func DirSize(root string) (int64, error) {
	if root == "" {
		return 0, nil
	}
	var total int64
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return total, err
	}
	return total, nil
}

// FormatBytes renders n as a human-readable size string suitable for
// picker display. Uses binary units (KiB/MiB/GiB) to match `du -h`.
// Values beyond the largest suffix are clamped to the top unit so
// pathological data dirs (>= 1 PiB) still render rather than panic.
func FormatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	suffixes := []string{"KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	if exp >= len(suffixes) {
		exp = len(suffixes) - 1
	}
	return fmt.Sprintf("%.1f %s", float64(n)/float64(div), suffixes[exp])
}
