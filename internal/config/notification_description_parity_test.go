package config

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"omakiten/defaults"

	"gopkg.in/yaml.v3"
)

// notificationShape mirrors the slice of bundled notification YAMLs
// needed for the parity check — slug-keyed description plus the action
// labels the footer renderer surfaces.
type notificationShape struct {
	Name        string                `yaml:"name"`
	Description string                `yaml:"description"`
	Actions     []notificationActionShape `yaml:"actions"`
}

type notificationActionShape struct {
	Key   string `yaml:"key"`
	ID    string `yaml:"id"`
	Label string `yaml:"label"`
}

// TestNotificationDescriptionsParity asserts that resolving every
// bundled notification's `description:` token against the en catalog
// returns the pre-migration literal recorded in the
// testdata/notification_description_golden/all.json golden.
//
// Per task #82 §17 / §41: the bundled YAMLs now carry
// `${{intl:notifications.<slug>.description}}` tokens instead of
// inline literals. Visible metadata must be byte-identical to the
// pre-migration state when the active language is en.
func TestNotificationDescriptionsParity(t *testing.T) {
	enLang := loadBundledLanguage(t, "en")
	catalog := NewCatalog(&enLang, &enLang)

	golden := loadNotificationDescriptionGolden(t)
	entries, err := fs.ReadDir(defaults.FS, "notifications")
	if err != nil {
		t.Fatalf("read bundled notifications: %v", err)
	}
	gotSlugs := map[string]struct{}{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		slug := strings.TrimSuffix(entry.Name(), ".yaml")
		raw, err := defaults.FS.ReadFile(filepath.ToSlash(filepath.Join("notifications", entry.Name())))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		var parsed notificationShape
		if err := yaml.Unmarshal(raw, &parsed); err != nil {
			t.Fatalf("unmarshal %s: %v", entry.Name(), err)
		}
		want, ok := golden[slug]
		if !ok {
			t.Errorf("notification %s has no golden entry; regenerate testdata/notification_description_golden/all.json", slug)
			continue
		}
		got := catalog.Resolve(parsed.Description)
		if got != want {
			t.Errorf("notification %s: resolved=%q, golden=%q", slug, got, want)
		}
		gotSlugs[slug] = struct{}{}
	}
	missing := []string{}
	for slug := range golden {
		if _, ok := gotSlugs[slug]; !ok {
			missing = append(missing, slug)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("golden has entries with no matching bundled YAML: %v", missing)
	}
}

// TestNotificationDescriptionsAllUseTokens enforces that every
// bundled notification's `description:` is an intl token, not an
// inline literal — same rationale as the preset variant: a regressed
// literal would silently round-trip through the resolver, defeating
// the parity guarantee.
func TestNotificationDescriptionsAllUseTokens(t *testing.T) {
	entries, err := fs.ReadDir(defaults.FS, "notifications")
	if err != nil {
		t.Fatalf("read bundled notifications: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		raw, err := defaults.FS.ReadFile(filepath.ToSlash(filepath.Join("notifications", entry.Name())))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		var parsed notificationShape
		if err := yaml.Unmarshal(raw, &parsed); err != nil {
			t.Fatalf("unmarshal %s: %v", entry.Name(), err)
		}
		if !strings.HasPrefix(parsed.Description, "${{intl:") {
			t.Errorf("notification %s description %q is not an intl token", entry.Name(), parsed.Description)
		}
	}
}

// TestNotificationActionLabelsAllUseTokens enforces that every bundled
// notification action label is an `${{intl:KEY}}` token, not an inline
// literal. The notification component resolves labels through the
// catalog at render time (Options.Catalog); a hardcoded label would
// bypass the catalog and surface the same literal in every locale —
// the exact regression that landed `${{intl:notifications.home-
// project-delete-confirm.confirm_label}}` on the Home delete overlay
// before the resolver wiring shipped.
func TestNotificationActionLabelsAllUseTokens(t *testing.T) {
	entries, err := fs.ReadDir(defaults.FS, "notifications")
	if err != nil {
		t.Fatalf("read bundled notifications: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		raw, err := defaults.FS.ReadFile(filepath.ToSlash(filepath.Join("notifications", entry.Name())))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		var parsed notificationShape
		if err := yaml.Unmarshal(raw, &parsed); err != nil {
			t.Fatalf("unmarshal %s: %v", entry.Name(), err)
		}
		for _, action := range parsed.Actions {
			if !strings.HasPrefix(action.Label, "${{intl:") {
				t.Errorf("notification %s action %q label %q is not an intl token; config files must reference catalog keys only", entry.Name(), action.ID, action.Label)
			}
		}
	}
}

// TestNotificationActionLabelsResolveAgainstEnCatalog asserts that
// every `${{intl:KEY}}` action label in the bundled notifications
// resolves to a non-literal string in the en catalog — i.e. the key
// exists in defaults/languages/en.yaml. Without this guard a renamed
// or typo'd key would silently fall through Catalog.Get's "return the
// key literal" path and surface the raw key in the footer.
func TestNotificationActionLabelsResolveAgainstEnCatalog(t *testing.T) {
	enLang := loadBundledLanguage(t, "en")
	catalog := NewCatalog(&enLang, &enLang)

	entries, err := fs.ReadDir(defaults.FS, "notifications")
	if err != nil {
		t.Fatalf("read bundled notifications: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		raw, err := defaults.FS.ReadFile(filepath.ToSlash(filepath.Join("notifications", entry.Name())))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		var parsed notificationShape
		if err := yaml.Unmarshal(raw, &parsed); err != nil {
			t.Fatalf("unmarshal %s: %v", entry.Name(), err)
		}
		for _, action := range parsed.Actions {
			resolved := catalog.Resolve(action.Label)
			if strings.Contains(resolved, "${{intl:") {
				t.Errorf("notification %s action %q label %q failed to resolve (token survived) — missing catalog entry", entry.Name(), action.ID, action.Label)
				continue
			}
			// Catalog.Get's missing-key fallback returns the bare key
			// literal (e.g. "notifications.foo.bar"). Detect that by
			// checking the resolved value equals the inner key text.
			inner := strings.TrimSuffix(strings.TrimPrefix(action.Label, "${{intl:"), "}}")
			if resolved == inner {
				t.Errorf("notification %s action %q label %q resolved to the bare key — missing catalog entry in defaults/languages/en.yaml", entry.Name(), action.ID, action.Label)
			}
		}
	}
}

func loadNotificationDescriptionGolden(t *testing.T) map[string]string {
	t.Helper()
	path := filepath.Join("testdata", "notification_description_golden", "all.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	var bySlug map[string]string
	if err := json.Unmarshal(raw, &bySlug); err != nil {
		t.Fatalf("decode golden %s: %v", path, err)
	}
	return bySlug
}
