package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ValidatePath accepts a path only when its lexical and physical location is
// below root. Existing symlinks are resolved for the physical check; missing
// leaves are allowed so a safe atomic writer can create them later.
func ValidatePath(root, path string) error {
	if root == "" || path == "" {
		return fmt.Errorf("root and path are required")
	}
	if filepath.IsAbs(path) {
		return fmt.Errorf("absolute paths are not allowed")
	}
	if hasParentComponent(path) {
		return fmt.Errorf("path contains parent traversal")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve root: %w", err)
	}
	pathAbs := filepath.Join(rootAbs, filepath.Clean(path))
	if err := requireRelativeTo(rootAbs, pathAbs); err != nil {
		return err
	}
	physicalRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return fmt.Errorf("resolve root physically: %w", err)
	}
	physicalPath, err := resolveExistingPrefix(pathAbs)
	if err != nil {
		return fmt.Errorf("resolve path physically: %w", err)
	}
	if err := requireRelativeTo(physicalRoot, physicalPath); err != nil {
		return fmt.Errorf("path escapes root physically: %w", err)
	}
	return nil
}

func hasParentComponent(path string) bool {
	for _, component := range strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' }) {
		if component == ".." {
			return true
		}
	}
	return false
}

func requireRelativeTo(root, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return fmt.Errorf("compare path with root: %w", err)
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("path is outside root")
	}
	return nil
}

func resolveExistingPrefix(path string) (string, error) {
	current := filepath.Clean(path)
	missing := make([]string, 0, 4)
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return resolved, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", os.ErrNotExist
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}
