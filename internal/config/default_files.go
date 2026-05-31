package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"omakiten/defaults"
)

// entityFolders lists the per-kind folders the layout expects as siblings of
// the config/ yaml dir. Order matters only for stable migration iteration.
var entityFolders = []string{"skills", "laws", "personas", "templates", "themes", "notifications", "languages"}

// EnsureDefaultFiles materializes the embedded default kit into a config root.
// Existing files are not overwritten; user-owned custom folders are created.
func EnsureDefaultFiles(rootDir string) error {
	if err := copyDefaultConfigProfiles(rootDir, false); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(rootDir, "config", "custom"), 0o755); err != nil {
		return fmt.Errorf("create config/custom: %w", err)
	}
	for _, sub := range entityFolders {
		if err := copyDefaultDir(rootDir, sub, false); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Join(rootDir, sub, "custom"), 0o755); err != nil {
			return fmt.Errorf("create %s/custom: %w", sub, err)
		}
	}
	return nil
}

// RefreshDefaultFiles overwrites every bundled default file while preserving custom/.
func RefreshDefaultFiles(rootDir string) error {
	if err := copyDefaultConfigProfiles(rootDir, true); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(rootDir, "config", "custom"), 0o755); err != nil {
		return fmt.Errorf("create config/custom: %w", err)
	}
	for _, sub := range entityFolders {
		if err := copyDefaultDir(rootDir, sub, true); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Join(rootDir, sub, "custom"), 0o755); err != nil {
			return fmt.Errorf("create %s/custom: %w", sub, err)
		}
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
			if err := os.MkdirAll(dst, 0o755); err != nil {
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
