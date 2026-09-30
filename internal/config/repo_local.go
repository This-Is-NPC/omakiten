package config

import (
	"fmt"
	"os"
	"path/filepath"

	"omakiten/internal/paths"
)

// RepoLocalDirName is the directory FindRepoLocal looks for during walk-up
// discovery. Convention mirrors git/eslint/prettier/cargo: a dot-prefixed
// directory committed at the repo root holds the project-specific install.
//
// When present, `.omakiten/` is the full config root for that project — it
// replaces the user-global ConfigRoot entirely (the only thing that stays
// global is the SQLite database).
const RepoLocalDirName = ".omakiten"

// FindRepoLocal walks up the directory tree starting at startDir looking
// for a directory named `.omakiten/`. Returns the absolute path of the
// first match plus true; returns ("", false, nil) when no match is found
// before hitting a stop boundary.
//
// Stop boundaries (after checking the current dir):
//   - $HOME — the walker never climbs above the user's home directory.
//   - Filesystem root — `filepath.Dir(cur) == cur`.
//
// A file (not a directory) named `.omakiten` at any level is treated as a
// miss so accidental files with that name do not capture the walk.
//
// startDir == "" is a no-op so callers without a meaningful CWD (background
// workers, tests) can opt out cheaply.
func FindRepoLocal(startDir string) (string, bool, error) {
	if startDir == "" {
		return "", false, nil
	}
	cur, err := filepath.Abs(startDir)
	if err != nil {
		return "", false, err
	}
	if err := paths.ValidateNoSymlinkComponents(cur); err != nil {
		return "", false, err
	}
	home := repoLocalHome()
	for {
		candidate, found, err := repoLocalAt(cur)
		if err != nil || found {
			return candidate, found, err
		}
		if home != "" && cur == home {
			return "", false, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", false, nil
		}
		cur = parent
	}
}

func repoLocalHome() string {
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		return ""
	}
	abs, err := filepath.Abs(h)
	if err != nil {
		return ""
	}
	return abs
}

func repoLocalAt(dir string) (string, bool, error) {
	candidate := filepath.Join(dir, RepoLocalDirName)
	info, err := os.Lstat(candidate)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", false, fmt.Errorf("refusing repo-local config root %s: symlink", candidate)
	}
	if !info.IsDir() {
		return "", false, nil
	}
	if err := paths.ValidateNoSymlinkComponents(candidate); err != nil {
		return "", false, err
	}
	return candidate, true, nil
}

// ValidateRepoLocalRoot rejects a repository-local config root or any of its
// existing path components when they are symlinks. Callers that create a local
// install use this before any materialization so a malicious pre-existing root
// cannot redirect writes into another tree.
func ValidateRepoLocalRoot(root string) error {
	return paths.ValidateNoSymlinkComponents(root)
}

// RepoLocalConfigFile returns the active config file of the repo-local
// install found walking up from startDir. found is false when no install
// is reachable.
func RepoLocalConfigFile(startDir string) (string, bool, error) {
	repoLocal, found, err := FindRepoLocal(startDir)
	if err != nil || !found {
		return "", false, err
	}
	path, err := paths.ActiveConfigFileInDir(filepath.Join(repoLocal, "config"))
	if err != nil {
		return "", false, err
	}
	return path, true, nil
}
