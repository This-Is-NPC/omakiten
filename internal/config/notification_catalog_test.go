package config

import (
	"path/filepath"
	"strings"
	"testing"

	"omakiten/defaults"
)

func TestBundledNotificationTextUsesCatalog(t *testing.T) {
	entries, err := defaults.FS.ReadDir("notifications")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			assertBundledNotificationText(t, entry.Name())
		})
	}
}

func assertBundledNotificationText(t *testing.T, name string) {
	t.Helper()
	path := filepath.ToSlash(filepath.Join("notifications", name))
	raw, err := defaults.FS.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	notification, err := decodeNotificationBytes(path, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(notification.Description, "${{intl:") {
		t.Errorf("description %q is not an intl token", notification.Description)
	}
	for _, action := range notification.Actions {
		if !strings.HasPrefix(action.Label, "${{intl:") {
			t.Errorf("action %q label %q is not an intl token", action.ID, action.Label)
		}
	}
}
