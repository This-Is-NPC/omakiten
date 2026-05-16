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
// needed for the parity check — just the slug-keyed description.
type notificationShape struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
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
