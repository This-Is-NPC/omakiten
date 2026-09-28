package paths

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var errNoYAMLFiles = errors.New("no yaml files")

var errUnsupportedActiveMarker = errors.New("active config marker requires a platform no-follow filesystem backend")

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

// ConfigRoot returns the base directory that holds both the yaml profile
// folder (config/) and every file-backed asset folder (personas/, laws/,
// skills/, templates/, themes/, notifications/, languages/) as siblings.
// Layout:
//
//	<root>/config/<profile>.yaml
//	<root>/<asset>/<slug-or-code>.<ext>          # defaults (overwritten on update)
//	<root>/<asset>/custom/<slug-or-code>.<ext>   # user-created entries (preserved)
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
// custom/ convention used by personas, laws, skills, templates, themes,
// notifications, and languages); when
// the active name is explicitly set via .active, the resolver tries that
// subtree first and only falls back to the config-dir root when nothing
// matches there. If the named profile is missing from both locations the
// resolver falls through to discovery rather than returning a stale path that
// would error at open time — so a removed or renamed canonical kit degrades
// to "first available .yaml" instead of breaking init.
func ActiveConfigFile() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return ActiveConfigFileInDir(dir)
}

// ActiveConfigFileInDir resolves the active yaml profile inside an explicit
// config directory. The selection is persisted in <dir>/.active — a one-line
// text file containing the basename of the chosen profile. When the file is
// missing or blank the resolver scans <dir>/ for the first .yaml file
// (alphabetical order) and falls back to <dir>/custom/ if none is found at
// the root. If no .yaml exists anywhere this function errors out — the caller
// needs a real path to open.
//
// Mirrors the user-global resolution discipline so a discovered .omakiten/
// install behaves identically to the global ConfigRoot: same .active rules,
// same custom/ shadow, same fall-through to discovery on a vanished kit.
func ActiveConfigFileInDir(dir string) (string, error) {
	if err := validateNoSymlinkComponents(dir); err != nil {
		return "", err
	}
	if path, found, err := activeConfigFromMarker(dir); err != nil {
		return "", err
	} else if found {
		return path, nil
	}
	return discoverConfigFile(dir)
}

func activeConfigFromMarker(dir string) (string, bool, error) {
	markerPath := filepath.Join(dir, ActiveConfigStateFile)
	markerInfo, err := os.Lstat(markerPath)
	if err == nil {
		if markerInfo.Mode()&os.ModeSymlink != 0 {
			return "", false, fmt.Errorf("refusing active config marker %s: symlink", markerPath)
		}
		if !markerInfo.Mode().IsRegular() {
			return "", false, fmt.Errorf("refusing active config marker %s: not a regular file", markerPath)
		}
		marker, readErr := readActiveConfigFile(dir)
		if readErr != nil {
			return "", false, readErr
		}
		// A traversal payload in .active ("../secret.yaml", "/abs/path",
		// "..") would otherwise resolve outside <dir>; reject and fall
		// through to discovery so a tampered state file degrades to the
		// same behaviour as a stale one.
		if name := strings.TrimSpace(string(marker)); validActiveConfigName(name) {
			return activeConfigNamedPath(dir, name)
		}
	} else if !os.IsNotExist(err) {
		return "", false, err
	}
	return "", false, nil
}

func activeConfigNamedPath(dir, name string) (string, bool, error) {
	for _, path := range []string{
		filepath.Join(dir, "custom", name),
		filepath.Join(dir, name),
	} {
		exists, err := regularConfigFile(path)
		if err != nil {
			return "", false, err
		}
		if exists {
			return path, true, nil
		}
	}
	return "", false, nil
}

func discoverConfigFile(dir string) (string, error) {
	name, firstErr := firstYAMLInDir(dir)
	if firstErr == nil {
		return filepath.Join(dir, name), nil
	}
	if !errors.Is(firstErr, errNoYAMLFiles) && !os.IsNotExist(firstErr) {
		return "", firstErr
	}
	name, customErr := firstYAMLInDir(filepath.Join(dir, "custom"))
	if customErr == nil {
		return filepath.Join(dir, "custom", name), nil
	}
	if !errors.Is(customErr, errNoYAMLFiles) && !os.IsNotExist(customErr) {
		return "", customErr
	}
	return "", fmt.Errorf("no config yaml found in %s or %s/custom", dir, dir)
}

// firstYAMLInDir returns the first file with a .yaml extension in dir,
// sorted alphabetically. Returns an error if dir does not exist or no .yaml
// is found.
func firstYAMLInDir(dir string) (string, error) {
	if err := validateNoSymlinkComponents(dir); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var names []string
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("refusing config path %s: symlink", path)
		}
		if info.IsDir() || !info.Mode().IsRegular() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(strings.ToLower(name), ".yaml") {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "", fmt.Errorf("%w in %s", errNoYAMLFiles, dir)
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
	if !validActiveConfigName(filename) {
		return fmt.Errorf("active config filename must be a clean basename, got %q", filename)
	}
	return setActiveConfigFile(dir, filename)
}

func regularConfigFile(path string) (bool, error) {
	if err := validateNoSymlinkComponents(path); err != nil {
		return false, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("refusing config path %s: symlink", path)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("refusing config path %s: not a regular file", path)
	}
	return true, nil
}

// validateNoSymlinkComponents rejects links in an explicit config path. The
// repo-local walker has the same check in internal/config, while this copy
// keeps the paths package independent of that adapter.
func validateNoSymlinkComponents(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	var components []string
	for cur := filepath.Clean(abs); ; cur = filepath.Dir(cur) {
		components = append(components, cur)
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
	}
	for index := len(components) - 1; index >= 0; index-- {
		component := components[index]
		info, err := os.Lstat(component)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("inspect path component %s: %w", component, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing path through symlink component %s", component)
		}
	}
	return nil
}

// validActiveConfigName reports whether name is safe to use as a yaml
// profile basename inside the config dir. Rejects traversal payloads
// (".", "..", absolute or nested paths, embedded separators) so neither
// the read path (ActiveConfigFileInDir) nor the write path
// (SetActiveConfigInDir) can be coaxed into resolving outside <dir>.
func validActiveConfigName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if name != filepath.Base(name) {
		return false
	}
	if strings.ContainsAny(name, `/\`) {
		return false
	}
	return true
}

// EntityDir resolves to <root>/<folder> — the directory holding the default
// file-backed assets (personas/, laws/, skills/, templates/, themes/,
// notifications/, languages/).
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

// StateDir returns the directory Omakiten owns under XDG_STATE_HOME.
// Used for ephemeral / recoverable artifacts (today: db backups) that
// belong neither in the user-edited config tree nor in the canonical
// data tree where the live DB lives. Resolution mirrors DataDir:
// $OMAKITEN_HOME takes precedence, then $XDG_STATE_HOME, then the
// per-OS default ~/.local/state/omakiten.
func StateDir() (string, error) {
	if base := os.Getenv(HomeEnv); base != "" {
		return filepath.Join(base, "state"), nil
	}
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, AppName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", AppName), nil
}

// BackupDir returns the directory `okt db backup` (and every destructive
// command that runs an auto-backup) writes snapshots into. Lives under
// StateDir/backups/ so the live DB tree stays untouched.
func BackupDir() (string, error) {
	dir, err := StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "backups"), nil
}
