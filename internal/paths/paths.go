package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ActiveConfigStateFile is the basename of the one-line state file that
// records which yaml profile is currently active. Lives next to the yaml
// files inside <root>/config/ so the entire config selection lives under
// the same directory.
const ActiveConfigStateFile = ".active"

// DefaultConfigFilename is the canonical default yaml profile that ships
// with the embed. Kept for backward compatibility when creating a fresh
// default; the resolver no longer hardcodes it as the only valid fallback.
const DefaultConfigFilename = "omakiten.yaml"

const AppName = "omakiten"

// HomeEnv is the env var that pins the entire Omakiten runtime (config + data)
// under a single directory. When set, it takes precedence over XDG and the
// per-user defaults. The expected layout is:
//
//	$OMAKITEN_HOME/config/<profile>.yaml
//	$OMAKITEN_HOME/data/omakiten.db
//	$OMAKITEN_HOME/<entity>/<slug>.md
//	$OMAKITEN_HOME/<entity>/custom/<slug>.md
//
// Useful for ephemeral dev environments and for users who want to keep all of
// Omakiten's state in one folder (e.g. on a thumb drive or inside a project).
const HomeEnv = "OMAKITEN_HOME"

// Resolution precedence for ConfigRoot, ConfigDir, and DataDir:
//   1. caller-supplied flags (handled outside this package)
//   2. $OMAKITEN_HOME (this package)
//   3. $XDG_CONFIG_HOME / $XDG_DATA_HOME
//   4. ~/.config/omakiten and ~/.local/share/omakiten

// ConfigRoot returns the base directory that holds both the yaml folder
// (config/) and every entity folder (personas/, laws/, skills/, templates/,
// themes/) as siblings. Layout:
//
//	<root>/config/<profile>.yaml
//	<root>/<entity>/<slug>.md            # default entries (overwritten on update)
//	<root>/<entity>/custom/<slug>.md     # user-created entries (preserved)
func ConfigRoot() (string, error) {
	if base := os.Getenv(HomeEnv); base != "" {
		return base, nil
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

// ConfigDir returns the directory that holds the active yaml profile and any
// sibling profile yamls. Always <root>/config across all resolution modes.
func ConfigDir() (string, error) {
	root, err := ConfigRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "config"), nil
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
	return ActiveConfigFile()
}

// ActiveConfigFile returns the absolute path of the yaml profile currently
// selected as active. The selection is persisted in <config-dir>/.active —
// a one-line text file containing the basename of the chosen profile. When
// the file is missing or blank the resolver scans <config-dir>/ for the
// first .yaml file (alphabetical order) and falls back to <config-dir>/custom/
// if none is found at the root. If no .yaml exists anywhere the app errors
// out — a config file is mandatory.
//
// User-authored profiles live under <config-dir>/custom/ (mirroring the
// custom/ convention used by personas, laws, skills, templates, themes); when
// the active name is explicitly set via .active, the resolver tries that
// subtree first and only falls back to the config-dir root when nothing
// matches there.
func ActiveConfigFile() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(dir, ActiveConfigStateFile))
	if err == nil {
		if name := strings.TrimSpace(string(data)); name != "" {
			customPath := filepath.Join(dir, "custom", name)
			if _, err := os.Stat(customPath); err == nil {
				return customPath, nil
			}
			return filepath.Join(dir, name), nil
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	// .active missing or blank — discover the first .yaml file.
	name, err := firstYAMLInDir(dir)
	if err == nil {
		return filepath.Join(dir, name), nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	// No yaml at root; try custom/.
	name, err = firstYAMLInDir(filepath.Join(dir, "custom"))
	if err == nil {
		return filepath.Join(dir, "custom", name), nil
	}
	if os.IsNotExist(err) {
		return "", fmt.Errorf("no config yaml found in %s or %s/custom", dir, dir)
	}
	return "", err
}

// firstYAMLInDir returns the first file with a .yaml extension in dir,
// sorted alphabetically. Returns an error if dir does not exist or no .yaml
// is found.
func firstYAMLInDir(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(strings.ToLower(name), ".yaml") {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no yaml files in %s", dir)
	}
	sort.Strings(names)
	return names[0], nil
}

// ConfigCustomDir returns <config-dir>/custom/ — the user-owned subtree for
// yaml profiles that should survive default refreshes. Mirrors the
// <entity>/custom convention.
func ConfigCustomDir() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "custom"), nil
}

// SetActiveConfig writes the state file so that subsequent calls to
// ActiveConfigFile / ConfigFile resolve to the chosen yaml profile. The
// caller must restart the runtime for the change to take effect — Omakiten
// loads the config exactly once during startup.
func SetActiveConfig(filename string) error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	return SetActiveConfigInDir(dir, filename)
}

// SetActiveConfigInDir writes the state file inside a specific config
// directory. Used when the caller knows the target directory (e.g. the
// project-local .omakiten/config/) rather than the global config root.
func SetActiveConfigInDir(dir, filename string) error {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return fmt.Errorf("active config filename is required")
	}
	if filename != filepath.Base(filename) {
		return fmt.Errorf("active config filename must be a basename, got %q", filename)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ActiveConfigStateFile), []byte(filename+"\n"), 0o644)
}

// EntityDir resolves to <root>/<folder> — the directory holding the default
// entity files (personas/, laws/, skills/, templates/, themes/).
func EntityDir(folder string) (string, error) {
	root, err := ConfigRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, folder), nil
}

// EntityCustomDir resolves to <root>/<folder>/custom — the user-owned subtree
// that survives every default refresh. Same-slug files in custom/ override the
// default entry at the loader level.
func EntityCustomDir(folder string) (string, error) {
	dir, err := EntityDir(folder)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "custom"), nil
}

func DatabaseFile() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "omakiten.db"), nil
}
