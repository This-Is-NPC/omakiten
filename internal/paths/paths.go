package paths

import (
	"os"
	"path/filepath"
)

const AppName = "omakiten"

// HomeEnv is the env var that pins the entire Omakiten runtime (config + data)
// under a single directory. When set, it takes precedence over XDG and the
// per-user defaults. The expected layout is:
//
//	$OMAKITEN_HOME/config/omakiten.yaml
//	$OMAKITEN_HOME/data/omakiten.db
//
// Useful for ephemeral dev environments and for users who want to keep all of
// Omakiten's state in one folder (e.g. on a thumb drive or inside a project).
const HomeEnv = "OMAKITEN_HOME"

// Resolution precedence for ConfigDir and DataDir:
//   1. caller-supplied flags (handled outside this package)
//   2. $OMAKITEN_HOME (this package)
//   3. $XDG_CONFIG_HOME / $XDG_DATA_HOME
//   4. ~/.config/omakiten and ~/.local/share/omakiten

func ConfigDir() (string, error) {
	if base := os.Getenv(HomeEnv); base != "" {
		return filepath.Join(base, "config"), nil
	}
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, AppName), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", AppName), nil
}

func DataDir() (string, error) {
	if base := os.Getenv(HomeEnv); base != "" {
		return filepath.Join(base, "data"), nil
	}
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, AppName), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", AppName), nil
}

func ConfigFile() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "omakiten.yaml"), nil
}

func DatabaseFile() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "omakiten.db"), nil
}
