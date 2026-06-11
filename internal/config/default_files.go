package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"omakiten/defaults"
	"omakiten/internal/paths"
)

// entityFolders lists the per-kind folders the layout expects as siblings of
// the config/ yaml dir. Order matters only for stable migration iteration.
var entityFolders = []string{"skills", "laws", "personas", "templates", "themes", "notifications", "languages"}

// hardenDir creates dir (and any missing parents) owner-only (0o700), but only
// tightens the mode of a directory it actually created. A pre-existing dir
// keeps its current mode and is never clobbered — even when it sits under the
// config root — because the same path may be a shared dir (e.g. ~/.config when
// --config points at a yaml outside an omakiten layout), and silently mutating
// a dir omakiten did not create is a surprising side-effect. This is the same
// rule WriteAtomic already follows: chmod only what you create.
//
// Net effect: a freshly created omakiten config subtree is owner-only (0o700)
// to match the 0o600 files inside it, closing the file-presence leak on first
// creation; a pre-existing dir is left untouched.
func hardenDir(dir string) error {
	_, statErr := os.Stat(dir)
	created := os.IsNotExist(statErr)
	if statErr != nil && !created {
		return statErr
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if created {
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// EnsureDefaultFiles materializes the embedded default kit into a config root.
// Existing files are not overwritten; user-owned custom folders are created.
func EnsureDefaultFiles(rootDir string) error {
	if err := hardenDir(rootDir); err != nil {
		return fmt.Errorf("harden config root: %w", err)
	}
	if err := copyDefaultConfigProfiles(rootDir, false); err != nil {
		return err
	}
	if err := hardenDir(filepath.Join(rootDir, "config", "custom")); err != nil {
		return fmt.Errorf("create config/custom: %w", err)
	}
	for _, sub := range entityFolders {
		if err := copyDefaultDir(rootDir, sub, false); err != nil {
			return err
		}
		if err := hardenDir(filepath.Join(rootDir, sub, "custom")); err != nil {
			return fmt.Errorf("create %s/custom: %w", sub, err)
		}
	}
	return nil
}

// RefreshDefaultFiles overwrites every bundled default file while preserving
// user-owned custom/ subtrees and config/.active. Managed files or directories
// outside custom/ that no longer exist in the embedded defaults are removed.
func RefreshDefaultFiles(rootDir string) error {
	if err := hardenDir(rootDir); err != nil {
		return fmt.Errorf("harden config root: %w", err)
	}
	if err := validateManagedDefaultDirBoundaries(rootDir); err != nil {
		return err
	}
	if err := pruneDefaultDir(filepath.Join(rootDir, "config"), "config", true); err != nil {
		return err
	}
	if err := copyDefaultConfigProfiles(rootDir, true); err != nil {
		return err
	}
	if err := hardenDir(filepath.Join(rootDir, "config", "custom")); err != nil {
		return fmt.Errorf("create config/custom: %w", err)
	}
	for _, sub := range entityFolders {
		if err := pruneDefaultDir(filepath.Join(rootDir, sub), sub, false); err != nil {
			return err
		}
		if err := copyDefaultDir(rootDir, sub, true); err != nil {
			return err
		}
		if err := hardenDir(filepath.Join(rootDir, sub, "custom")); err != nil {
			return fmt.Errorf("create %s/custom: %w", sub, err)
		}
	}
	return nil
}

// ValidateDefaultRefreshRoot confirms a root selected by a destructive
// refresh-defaults CLI invocation looks like an existing Omakiten install.
// Refresh is deliberately not setup: callers must seed an install first.
func ValidateDefaultRefreshRoot(rootDir, configPath string) error {
	root, err := filepath.Abs(rootDir)
	if err != nil {
		return err
	}
	configDir := filepath.Join(root, "config")
	info, err := os.Lstat(configDir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s is not an Omakiten install root: missing config directory", root)
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is not an Omakiten install root: config is a symlink", root)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not an Omakiten install root: config is not a directory", root)
	}
	if configPath != "" {
		absConfig, err := filepath.Abs(configPath)
		if err != nil {
			return err
		}
		if !pathWithinDir(configDir, absConfig) {
			return fmt.Errorf("config path %s is outside %s", absConfig, configDir)
		}
	}
	if ok, err := defaultRefreshRootHasMarker(configDir); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%s is not an Omakiten install root: missing config/.active, config/custom, or config/modules", root)
	}
	return validateManagedDefaultDirBoundaries(root)
}

func defaultRefreshRootHasMarker(configDir string) (bool, error) {
	for _, name := range []string{paths.ActiveConfigStateFile, "custom", "modules"} {
		_, err := os.Lstat(filepath.Join(configDir, name))
		if err == nil {
			return true, nil
		}
		if !os.IsNotExist(err) {
			return false, err
		}
	}
	return false, nil
}

func pathWithinDir(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

// validateManagedDefaultDirBoundaries Lstat-validates each top-level managed
// dir (config + entityFolders) once, up front, before any prune/copy runs.
//
// TOCTOU note: there is a window between this one-shot validation and the
// later prune/copy during which an attacker who already controls the config
// root could swap a validated dir for a symlink. That window is mostly closed
// because the prune walk does NOT blindly trust this pre-check: pruneDefaultDir
// re-derives every path under the (already Lstat-validated) top-level dir and
// RefreshDefaultFiles re-hardens dirs as it goes, so a swapped top-level entry
// would have to win the race against an attacker who can already write inside
// the user's own config root — a strictly weaker position than the boundary
// this guard exists to enforce. We deliberately keep the validation one-shot
// (rather than re-Lstat'ing inside the prune loop) because the recursive prune
// operates on os.DirEntry results from the parent's ReadDir, which never
// follow the top-level symlink we rejected here; adding a per-iteration re-stat
// would not close the residual window without a full open-by-handle rewrite.
func validateManagedDefaultDirBoundaries(rootDir string) error {
	for _, sub := range append([]string{"config"}, entityFolders...) {
		if err := validateManagedDefaultDirBoundary(filepath.Join(rootDir, sub)); err != nil {
			return err
		}
	}
	return nil
}

func validateManagedDefaultDirBoundary(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink; refusing to refresh managed defaults through it", dir)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory; refusing to refresh managed defaults through it", dir)
	}
	return nil
}

// pruneDefaultDir removes managed (non-custom, non-.active) entries from dstDir
// that no longer match the embedded defaults, ahead of the recopy that follows
// in RefreshDefaultFiles.
//
// Half-pruned window: prune and recopy are NOT atomic. If pruneDefaultDirContents
// fails partway (e.g. an I/O error mid-walk), the install can be left with some
// managed files removed and the fresh copies not yet written. This is accepted,
// not a leak: the binary swap that triggered the refresh is already durable, the
// CLI surfaces the idempotent repair command (`okt config refresh-defaults`),
// and re-running the refresh is safe — prune skips already-removed entries and
// recopy overwrites whatever remains, converging the install to the embedded
// defaults regardless of where the prior run stopped. No rollback/staging is
// added because the recovery path (re-run) is strictly simpler and equally
// durable.
func pruneDefaultDir(dstDir, srcDir string, preserveActive bool) error {
	if err := pruneDefaultDirContents(dstDir, filepath.ToSlash(srcDir), preserveActive); err != nil {
		return fmt.Errorf("prune managed defaults in %s: %w", dstDir, err)
	}
	return nil
}

func pruneDefaultDirContents(dstDir, srcDir string, preserveActive bool) error {
	entries, err := os.ReadDir(dstDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "custom" {
			continue
		}
		if preserveActive && name == paths.ActiveConfigStateFile {
			continue
		}

		dstPath := filepath.Join(dstDir, name)
		srcPath := srcDir + "/" + name
		srcInfo, statErr := fs.Stat(defaults.FS, srcPath)
		srcExists := statErr == nil
		if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
			return fmt.Errorf("stat embedded %s: %w", srcPath, statErr)
		}

		if entry.IsDir() {
			if srcExists && !srcInfo.IsDir() {
				if err := os.RemoveAll(dstPath); err != nil {
					return err
				}
				continue
			}
			if err := pruneDefaultDirContents(dstPath, srcPath, false); err != nil {
				return err
			}
			if !srcExists {
				if err := removeDirIfEmpty(dstPath); err != nil {
					return err
				}
			}
			continue
		}

		if !srcExists || srcInfo.IsDir() {
			if err := os.Remove(dstPath); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}

func removeDirIfEmpty(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(entries) != 0 {
		return nil
	}
	if err := os.Remove(dir); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func copyDefaultConfigProfiles(rootDir string, overwrite bool) error {
	return copyEmbeddedDirRecursive("config", filepath.Join(rootDir, "config"), overwrite)
}

// copyEmbeddedDirRecursive copies every file under srcDir in defaults.FS to
// the corresponding path under dstDir, creating subdirectories as needed.
// Directories in srcDir are traversed recursively; the custom/ skip used by
// entity folders is intentionally absent here because config/modules/ and
// config/themes/ are bundled directories that should always be materialised.
func copyEmbeddedDirRecursive(srcDir, dstDir string, overwrite bool) error {
	entries, err := defaults.FS.ReadDir(srcDir)
	if err != nil {
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			return nil
		}
		return fmt.Errorf("read embedded %s: %w", srcDir, err)
	}
	for _, entry := range entries {
		src := srcDir + "/" + entry.Name()
		dst := filepath.Join(dstDir, entry.Name())
		if entry.IsDir() {
			if err := hardenDir(dst); err != nil {
				return fmt.Errorf("create %s: %w", dst, err)
			}
			if err := copyEmbeddedDirRecursive(src, dst, overwrite); err != nil {
				return err
			}
			continue
		}
		if overwrite {
			if err := copyDefaultOverwrite(dst, src); err != nil {
				return err
			}
		} else {
			if err := copyDefaultIfMissing(dst, src); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyDefaultDir(rootDir, sub string, overwrite bool) error {
	entries, err := defaults.FS.ReadDir(sub)
	if err != nil {
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			return nil
		}
		return fmt.Errorf("read embedded %s: %w", sub, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		src := sub + "/" + entry.Name()
		dst := filepath.Join(rootDir, sub, entry.Name())
		if overwrite {
			if err := copyDefaultOverwrite(dst, src); err != nil {
				return err
			}
		} else {
			if err := copyDefaultIfMissing(dst, src); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyDefaultIfMissing(dstPath, srcPath string) error {
	if _, err := os.Stat(dstPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return copyDefaultOverwrite(dstPath, srcPath)
}

func copyDefaultOverwrite(dstPath, srcPath string) error {
	data, err := defaults.FS.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read default %s: %w", srcPath, err)
	}
	return WriteAtomic(dstPath, data)
}
