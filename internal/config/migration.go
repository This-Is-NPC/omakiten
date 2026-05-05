package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"omakiten/defaults"
)

// configDefaultFilename mirrors paths.DefaultConfigFilename without forcing a
// dependency from the config package onto paths. Keep in sync.
const configDefaultFilename = "omakiten.yaml"

// MigrateLayout normalizes a config root from any prior layout to the current
// one, idempotently. Layout history:
//
//   - v0 (flat, XDG/default mode): omakiten.yaml + entity folders at <root>/.
//   - v1 (early OMAKITEN_HOME mode): yaml + entity folders all under <root>/config/.
//   - v2 (current): <root>/config/omakiten.yaml + entity folders at <root>/<kind>/
//     with a custom/ subtree under each.
//
// Effects on call:
//
//   - If <root>/omakiten.yaml exists, move it to <root>/config/omakiten.yaml.
//   - If <root>/config/<kind>/ exists for a known kind, move its contents up to
//     <root>/<kind>/ (creating the destination if needed).
//   - For each entity kind, files whose filename does not match an embedded
//     default are user-created and get moved into <root>/<kind>/custom/.
//     Default-slug files at the root are left untouched — they will be
//     refreshed by the next install/refresh; the user accepts that direct
//     edits to those files are lost.
//   - Missing custom/ subdirs are created (empty).
//
// Returns nil when the layout is already current (no work to do).
func MigrateLayout(rootDir string) error {
	if err := migrateYAML(rootDir); err != nil {
		return err
	}
	if err := migrateEntityFolders(rootDir); err != nil {
		return err
	}
	if err := segregateUserCustoms(rootDir); err != nil {
		return err
	}
	if err := segregateUserConfigProfiles(rootDir); err != nil {
		return err
	}
	return nil
}

func migrateYAML(rootDir string) error {
	legacy := filepath.Join(rootDir, "omakiten.yaml")
	current := filepath.Join(rootDir, "config", "omakiten.yaml")

	if _, err := os.Stat(legacy); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if _, err := os.Stat(current); err == nil {
		// New location already populated — drop the stale flat copy.
		return os.Remove(legacy)
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.MkdirAll(filepath.Join(rootDir, "config"), 0o755); err != nil {
		return fmt.Errorf("create config/: %w", err)
	}
	if err := os.Rename(legacy, current); err != nil {
		return fmt.Errorf("move omakiten.yaml: %w", err)
	}
	return nil
}

func migrateEntityFolders(rootDir string) error {
	for _, kind := range entityFolders {
		legacyDir := filepath.Join(rootDir, "config", kind)
		currentDir := filepath.Join(rootDir, kind)

		entries, err := os.ReadDir(legacyDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("read legacy %s: %w", legacyDir, err)
		}
		if len(entries) == 0 {
			_ = os.Remove(legacyDir)
			continue
		}

		if err := os.MkdirAll(currentDir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", currentDir, err)
		}
		for _, entry := range entries {
			from := filepath.Join(legacyDir, entry.Name())
			to := filepath.Join(currentDir, entry.Name())
			// New layout wins: if a same-named target already exists at the
			// current location, drop the legacy copy so the source-of-truth is
			// unambiguous. Sub-directories at the legacy root are recursively
			// removed (they belong to the old shape and have no role in v2).
			if _, err := os.Stat(to); err == nil {
				if err := os.RemoveAll(from); err != nil {
					return fmt.Errorf("remove redundant legacy %s: %w", from, err)
				}
				continue
			} else if !os.IsNotExist(err) {
				return err
			}
			if err := os.Rename(from, to); err != nil {
				return fmt.Errorf("move %s -> %s: %w", from, to, err)
			}
		}
		// Now that all entries have been either moved or removed, the legacy dir
		// should be empty. Best-effort cleanup; ignore errors.
		_ = os.Remove(legacyDir)
	}
	return nil
}

func segregateUserCustoms(rootDir string) error {
	for _, kind := range entityFolders {
		dir := filepath.Join(rootDir, kind)
		if _, err := os.Stat(dir); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}

		defaultsSet, err := embeddedDefaultFilenames(kind)
		if err != nil {
			return err
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			return fmt.Errorf("read %s: %w", dir, err)
		}
		customDir := filepath.Join(dir, "custom")
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if _, isDefault := defaultsSet[name]; isDefault {
				continue
			}
			// User-created entry → move into custom/.
			if err := os.MkdirAll(customDir, 0o755); err != nil {
				return fmt.Errorf("create %s/custom: %w", dir, err)
			}
			from := filepath.Join(dir, name)
			to := filepath.Join(customDir, name)
			if _, err := os.Stat(to); err == nil {
				// Custom already has a file with this name — leave both alone.
				continue
			} else if !os.IsNotExist(err) {
				return err
			}
			if err := os.Rename(from, to); err != nil {
				return fmt.Errorf("move %s -> custom/: %w", from, err)
			}
		}
		// Always make sure custom/ exists, even when nothing got moved, so the
		// user has a clear place to drop new files.
		if err := os.MkdirAll(customDir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", customDir, err)
		}
	}
	return nil
}

// segregateUserConfigProfiles relocates yaml profiles other than the
// canonical default into <config-dir>/custom, mirroring the entity folder
// convention. The canonical default `omakiten.yaml` stays at the root (it
// is overwritten by every refresh); state files (`.active`) and any non-yaml
// content at the root are left untouched.
func segregateUserConfigProfiles(rootDir string) error {
	configDir := filepath.Join(rootDir, "config")
	entries, err := os.ReadDir(configDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", configDir, err)
	}
	customDir := filepath.Join(configDir, "custom")
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == configDefaultFilename {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(name), ".yaml") {
			continue
		}
		if err := os.MkdirAll(customDir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", customDir, err)
		}
		from := filepath.Join(configDir, name)
		to := filepath.Join(customDir, name)
		if _, err := os.Stat(to); err == nil {
			// Custom already has a profile with this name — leave the root copy
			// where it is to avoid surprising the user. Cleanup is manual.
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.Rename(from, to); err != nil {
			return fmt.Errorf("move %s -> custom/: %w", from, err)
		}
	}
	// Always make sure custom/ exists so the user has somewhere obvious to drop
	// new profiles, even when nothing got moved.
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", customDir, err)
	}
	return nil
}

func embeddedDefaultFilenames(kind string) (map[string]struct{}, error) {
	out := map[string]struct{}{}
	entries, err := defaults.FS.ReadDir(kind)
	if err != nil {
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			return out, nil
		}
		return nil, fmt.Errorf("read embedded %s: %w", kind, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		out[entry.Name()] = struct{}{}
	}
	return out, nil
}
