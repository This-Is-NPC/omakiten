package config

import (
	"os"
	"path/filepath"
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
	home := ""
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		if abs, err := filepath.Abs(h); err == nil {
			home = abs
		}
	}
	for {
		candidate := filepath.Join(cur, RepoLocalDirName)
		info, err := os.Stat(candidate)
		switch {
		case err == nil && info.IsDir():
			return candidate, true, nil
		case err != nil && !os.IsNotExist(err):
			return "", false, err
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
