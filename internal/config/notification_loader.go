package config

import (
	"bytes"
	"fmt"

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
	return decodeNotificationBytes(path, raw)
}

func decodeNotificationBytes(path string, raw []byte) (Notification, error) {
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
	items, warnings, err := LoadFromDir(dir, LoadOptions[Notification]{
		Suffixes:     []string{".yaml", ".yml"},
		MaxFileBytes: MaxNotificationFileBytes,
		Decode: func(path string, raw []byte, isCustom bool) (Notification, *SourceWarning, error) {
			notification, derr := decodeNotificationBytes(path, raw)
			if derr != nil {
				return Notification{}, nil, derr
			}
			notification.IsCustom = isCustom
			return notification, nil, nil
		},
		SlugOf:    func(n Notification) string { return n.Name },
		Collision: CollideOverwrite,
		OnDecodeError: func(path string, isCustom bool, derr error) (*SourceWarning, bool) {
			if !isCustom {
				return nil, false
			}
			return &SourceWarning{
				Path:    path,
				Message: fmt.Sprintf("custom notification skipped — file is incompatible with the current schema: %v", derr),
			}, true
		},
	})
	if err != nil {
		return nil, nil, err
	}
	byName := make(map[string]Notification, len(items))
	for _, n := range items {
		byName[n.Name] = n
	}
	return byName, warnings, nil
}
