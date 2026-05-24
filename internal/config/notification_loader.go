package config

import (
	"bytes"
	"fmt"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadNotification reads a single notification YAML, decodes with KnownFields(true),
// and runs ValidateNotification. The on-disk shape mirrors theme files —
// pure YAML, no frontmatter wrapping.
func LoadNotification(path string) (Notification, error) {
	raw, err := readFileBounded(path, MaxNotificationFileBytes)
	if err != nil {
		return Notification{}, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
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

// LoadNotifications discovers every *.yaml under dir and dir/custom and
// returns them keyed by Notification.Name. Custom files override defaults
// that share a name. Returns an empty map when dir is missing — the
// runtime treats "no notifications" as "no notification hooks available" and the
// hooks-validator step rejects `notification:` references accordingly.
//
// Default-scope files MUST be valid — a parse or validation error at
// the default scope is fatal. Custom-scope files are user-owned and
// may drift from the current schema; loading errors there are
// surfaced as SourceWarnings instead of failing the whole bundle, so
// the app stays usable while clearly flagging which custom files
// were skipped.
func LoadNotifications(dir string) (map[string]Notification, []SourceWarning, error) {
	files, err := listNotificationFiles(dir)
	if err != nil {
		return nil, nil, err
	}

	byName := map[string]Notification{}
	seen := map[string]string{}
	var warnings []SourceWarning
	for _, file := range files {
		notification, err := LoadNotification(file.Path)
		if err != nil {
			if file.IsCustom {
				warnings = append(warnings, SourceWarning{
					Path:    file.Path,
					Message: fmt.Sprintf("custom notification skipped — file is incompatible with the current schema: %v", err),
				})
				continue
			}
			return nil, nil, err
		}
		notification.IsCustom = file.IsCustom
		if previous, dup := seen[notification.Name]; dup {
			previousIsCustom := byName[notification.Name].IsCustom
			if previousIsCustom == file.IsCustom {
				return nil, nil, fmt.Errorf("duplicate notification name %q (also defined in %s)", notification.Name, previous)
			}
			// Custom overrides default — only when the new file is
			// custom AND the existing entry was a default.
			if !file.IsCustom {
				continue
			}
		}
		seen[notification.Name] = file.Path
		byName[notification.Name] = notification
	}
	return byName, warnings, nil
}

// notificationFile is an alias for the shared entityFile shape;
// notifications + entities + language packs all walk the same
// {Path, IsCustom} pair, so the loader code dedups via listFilesIn.
type notificationFile = entityFile

func listNotificationFiles(dir string) ([]notificationFile, error) {
	defaults, err := readYAMLFilesIn(dir, false)
	if err != nil {
		return nil, err
	}
	customs, err := readYAMLFilesIn(filepath.Join(dir, "custom"), true)
	if err != nil {
		return nil, err
	}
	return append(defaults, customs...), nil
}

func readYAMLFilesIn(dir string, isCustom bool) ([]notificationFile, error) {
	return listFilesIn(dir, []string{".yaml", ".yml"}, isCustom)
}
