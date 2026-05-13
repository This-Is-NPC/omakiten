package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadNotification reads a single notification YAML, decodes with KnownFields(true),
// and runs ValidateNotification. The on-disk shape mirrors theme files —
// pure YAML, no frontmatter wrapping.
func LoadNotification(path string) (Notification, error) {
	file, err := os.Open(path)
	if err != nil {
		return Notification{}, err
	}
	defer func() { _ = file.Close() }()

	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)

	var notification Notification
	if err := decoder.Decode(&notification); err != nil {
		return Notification{}, fmt.Errorf("decode %s: %w", path, err)
	}
	notification.SourcePath = path
	if err := ValidateNotification(notification); err != nil {
		return Notification{}, err
	}
	return notification, nil
}

// LoadNotifications discovers every *.yaml under defaultDir, defaultDir/custom
// and an optional repoLocalDir (flat) and returns them keyed by
// Notification.Name. Layer precedence resolves name collisions:
// repo-local > custom > default. Returns an empty map when no source
// contributes files — the runtime treats "no notifications" as "no
// notification hooks available" and the hooks-validator step rejects
// `notification:` references accordingly.
//
// Default-scope files MUST be valid — a parse or validation error at
// the default scope is fatal. Custom-scope and repo-local-scope files
// are user-owned and may drift from the current schema; loading errors
// there are surfaced as SourceWarnings instead of failing the whole
// bundle, so the app stays usable while clearly flagging which files
// were skipped.
func LoadNotifications(defaultDir, repoLocalDir string) (map[string]Notification, []SourceWarning, error) {
	files, err := listNotificationFiles(defaultDir, repoLocalDir)
	if err != nil {
		return nil, nil, err
	}

	byName := map[string]Notification{}
	seen := map[string]notificationFile{}
	var warnings []SourceWarning
	for _, file := range files {
		notification, err := LoadNotification(file.Path)
		if err != nil {
			if file.Layer != layerDefault {
				warnings = append(warnings, SourceWarning{
					Path:    file.Path,
					Message: fmt.Sprintf("notification skipped — file is incompatible with the current schema: %v", err),
				})
				continue
			}
			return nil, nil, err
		}
		notification.IsCustom = file.Layer == layerCustom
		notification.IsRepoLocal = file.Layer == layerRepoLocal
		if previous, dup := seen[notification.Name]; dup {
			if previous.Layer == file.Layer {
				return nil, nil, fmt.Errorf("duplicate notification name %q (also defined in %s)", notification.Name, previous.Path)
			}
			// Lower-layer entry already loaded; the new (higher-layer)
			// file overrides it.
		}
		seen[notification.Name] = file
		byName[notification.Name] = notification
	}
	return byName, warnings, nil
}

type notificationFile struct {
	Path  string
	Layer entityLayer
}

func listNotificationFiles(defaultDir, repoLocalDir string) ([]notificationFile, error) {
	defaults, err := readYAMLFilesIn(defaultDir, layerDefault)
	if err != nil {
		return nil, err
	}
	customs, err := readYAMLFilesIn(filepath.Join(defaultDir, "custom"), layerCustom)
	if err != nil {
		return nil, err
	}
	files := append(defaults, customs...)
	if repoLocalDir != "" {
		repoLocal, err := readYAMLFilesIn(repoLocalDir, layerRepoLocal)
		if err != nil {
			return nil, err
		}
		files = append(files, repoLocal...)
	}
	return files, nil
}

func readYAMLFilesIn(dir string, layer entityLayer) ([]notificationFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}
	var files []notificationFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".yaml") && !strings.HasSuffix(lower, ".yml") {
			continue
		}
		files = append(files, notificationFile{Path: filepath.Join(dir, name), Layer: layer})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}
