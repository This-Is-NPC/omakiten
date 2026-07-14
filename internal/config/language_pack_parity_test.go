package config

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode"

	"omakiten/defaults"
)

// TestBundledLanguagePacksHaveIdenticalKeySets enforces task #82 §43:
// every shipped language pack under defaults/languages must declare
// exactly the same key set as the en baseline. Drift in either
// direction is a bug: a missing pt-br key would silently fall back to
// en at runtime (acceptable behavior but not authorial intent), and
// an extra pt-br key signals dead translation effort.
//
// The check loads each pack via the loader's strict decoder so YAML
// errors surface at test time, not at boot in a user shell.
func TestBundledLanguagePacksHaveIdenticalKeySets(t *testing.T) {
	en := loadBundledLanguage(t, "en")
	enKeys := keySet(en)

	entries, err := defaults.FS.ReadDir("languages")
	if err != nil {
		t.Fatalf("read bundled languages: %v", err)
	}
	for _, key := range []string{"cli.db.reindex.warning.backup", "cli.db.reindex.error.confirm_required"} {
		if strings.Count(en.Keys[key], "%s") != 1 {
			t.Errorf("English %s placeholder count = %d, want 1", key, strings.Count(en.Keys[key], "%s"))
		}
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		code := strings.TrimSuffix(entry.Name(), ".yaml")
		if code == "en" {
			continue
		}
		lang := loadBundledLanguage(t, code)
		other := keySet(lang)

		missing := setDiff(enKeys, other)
		extra := setDiff(other, enKeys)
		if len(missing) == 0 && len(extra) == 0 {
			continue
		}
		if len(missing) > 0 {
			t.Errorf("language pack %s missing %d keys (first 5): %v", code, len(missing), firstN(missing, 5))
		}
		if len(extra) > 0 {
			t.Errorf("language pack %s has %d extra keys (first 5): %v", code, len(extra), firstN(extra, 5))
		}
	}
}

func TestBundledDBMaintenanceStringsAreTranslated(t *testing.T) {
	t.Parallel()

	keys := []string{
		"cli.db.check.short",
		"cli.db.check.long",
		"cli.db.reindex.short",
		"cli.db.reindex.long",
		"cli.db.check.error.invalid",
		"cli.db.reindex.warning.backup",
		"cli.db.reindex.flag.confirm",
		"cli.db.reindex.error.confirm_required",
		"cli.db.error.missing_fmt",
		"cli.db.error.not_file_fmt",
	}
	en := loadBundledLanguage(t, "en")
	if !strings.Contains(en.Keys["cli.db.reindex.long"], "verified rolling backup") {
		t.Error("English cli.db.reindex.long does not describe the verified pre-repair backup")
	}
	if !strings.Contains(en.Keys["cli.db.reindex.warning.backup"], "verified pre-repair backup") {
		t.Error("English cli.db.reindex.warning.backup does not identify the verified backup path")
	}
	entries, err := defaults.FS.ReadDir("languages")
	if err != nil {
		t.Fatalf("read bundled languages: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "en.yaml" || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		code := strings.TrimSuffix(entry.Name(), ".yaml")
		t.Run(code, func(t *testing.T) {
			lang := loadBundledLanguage(t, code)
			for _, key := range keys {
				if lang.Keys[key] == en.Keys[key] {
					t.Errorf("%s still copies the English value", key)
				}
				if substantiveLanguageText(lang.Keys[key]) == substantiveLanguageText(en.Keys[key]) {
					t.Errorf("%s differs from English only by punctuation or spacing", key)
				}
				if strings.Count(lang.Keys[key], "%s") != strings.Count(en.Keys[key], "%s") {
					t.Errorf("%s %%s placeholder count drifted", key)
				}
			}
			for _, token := range []string{"FTS5", "task", "comment", "error", "solution", "plan"} {
				if !strings.Contains(lang.Keys["cli.db.check.long"], token) {
					t.Errorf("cli.db.check.long missing token %q", token)
				}
			}
			for _, key := range []string{"cli.db.reindex.warning.backup", "cli.db.reindex.error.confirm_required"} {
				if strings.Count(lang.Keys[key], "%s") != 1 {
					t.Errorf("%s placeholder count = %d, want 1", key, strings.Count(lang.Keys[key], "%s"))
				}
			}
		})
	}
}

func substantiveLanguageText(value string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || r == '%' {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func keySet(lang Language) map[string]struct{} {
	out := make(map[string]struct{}, len(lang.Keys))
	for key := range lang.Keys {
		out[key] = struct{}{}
	}
	return out
}

func setDiff(a, b map[string]struct{}) []string {
	out := []string{}
	for key := range a {
		if _, ok := b[key]; !ok {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

func firstN(values []string, n int) []string {
	if len(values) < n {
		return values
	}
	return values[:n]
}

// TestEnglishCatalogResolvesAllBundledTokens scans every YAML under
// defaults/config/ + defaults/notifications/ for ${{intl:KEY}} tokens
// and asserts each KEY is defined in the en catalog. Catches the
// "added a token in YAML but forgot the en.yaml entry" mistake which
// would silently render the key literal at runtime.
func TestEnglishCatalogResolvesAllBundledTokens(t *testing.T) {
	en := loadBundledLanguage(t, "en")
	missing := []string{}
	for _, dir := range []string{"config", "notifications"} {
		entries, err := defaults.FS.ReadDir(dir)
		if err != nil {
			t.Fatalf("read bundled %s: %v", dir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
				continue
			}
			path := filepath.ToSlash(filepath.Join(dir, entry.Name()))
			raw, err := defaults.FS.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			for _, key := range extractIntlKeys(string(raw)) {
				if _, ok := en.Keys[key]; !ok {
					missing = append(missing, path+" → "+key)
				}
			}
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("bundled YAMLs reference %d undeclared intl keys (first 5): %v", len(missing), firstN(missing, 5))
	}
}

func extractIntlKeys(s string) []string {
	out := []string{}
	for {
		idx := strings.Index(s, "${{intl:")
		if idx < 0 {
			return out
		}
		s = s[idx+len("${{intl:"):]
		end := strings.Index(s, "}}")
		if end < 0 {
			return out
		}
		out = append(out, s[:end])
		s = s[end+2:]
	}
}
