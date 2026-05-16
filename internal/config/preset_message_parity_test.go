package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/defaults"

	"gopkg.in/yaml.v3"
)

// presets enumerates the bundled workflow presets whose hook
// `message:` fields were migrated from inline literals into
// `${{intl:notifications.<preset>.<seq>.message}}` tokens. Each entry
// has a matching golden file under testdata/preset_message_golden/
// carrying the pre-migration message text in positional order.
var presets = []string{"izakaya", "kaiseki", "omakase", "shokunin"}

// presetHookShape mirrors the minimal slice of the bundled preset YAML
// needed for the parity check — just config.hooks[*].message.
type presetHookShape struct {
	Config struct {
		Hooks []struct {
			Message string `yaml:"message"`
		} `yaml:"hooks"`
	} `yaml:"config"`
}

// TestPresetHookMessagesParity loads the en catalog from
// defaults/languages/en.yaml plus each bundled preset YAML, then
// asserts that resolving every hook message against the catalog
// returns the original literal recorded in the per-preset JSON
// golden under testdata/preset_message_golden/.
//
// This is the load-bearing parity test for task #82 §40: the
// migration replaced literal `message:` values with intl tokens; the
// catalog ships the original literals under those keys. Visible
// notification behavior must be byte-identical to the pre-migration
// state when the active language is en.
func TestPresetHookMessagesParity(t *testing.T) {
	enLang := loadBundledLanguage(t, "en")
	catalog := NewCatalog(&enLang, &enLang)

	for _, preset := range presets {
		golden := loadPresetGolden(t, preset)
		raw := readBundledPreset(t, preset)
		var parsed presetHookShape
		if err := yaml.Unmarshal(raw, &parsed); err != nil {
			t.Fatalf("unmarshal preset %s: %v", preset, err)
		}
		if len(parsed.Config.Hooks) != len(golden) {
			t.Fatalf("preset %s: hooks count=%d, golden count=%d (regenerate testdata/preset_message_golden/%s.json after reordering hooks)", preset, len(parsed.Config.Hooks), len(golden), preset)
		}
		for i, hook := range parsed.Config.Hooks {
			got := catalog.Resolve(hook.Message)
			if got != golden[i] {
				t.Errorf("preset %s hook[%d]: resolved=%q, golden=%q", preset, i, got, golden[i])
			}
		}
	}
}

// TestPresetHookMessagesAllUseTokens guards the migration's other
// direction: every bundled preset hook `message:` field must be a
// `${{intl:...}}` token, never an inline literal. A regression that
// re-introduces a literal would silently pass the parity test (the
// literal resolves to itself) so this side-check keeps the catalog
// the single source of truth for preset chrome.
func TestPresetHookMessagesAllUseTokens(t *testing.T) {
	for _, preset := range presets {
		raw := readBundledPreset(t, preset)
		var parsed presetHookShape
		if err := yaml.Unmarshal(raw, &parsed); err != nil {
			t.Fatalf("unmarshal preset %s: %v", preset, err)
		}
		for i, hook := range parsed.Config.Hooks {
			if !strings.HasPrefix(hook.Message, "${{intl:") {
				t.Errorf("preset %s hook[%d] message %q is not an intl token; the catalog must own preset chrome", preset, i, hook.Message)
			}
		}
	}
}

func loadBundledLanguage(t *testing.T, code string) Language {
	t.Helper()
	raw, err := defaults.FS.ReadFile(filepath.ToSlash(filepath.Join("languages", code+".yaml")))
	if err != nil {
		t.Fatalf("read bundled %s.yaml: %v", code, err)
	}
	var lf languageFile
	if err := decodeLanguageStrict(raw, &lf); err != nil {
		t.Fatalf("decode bundled %s.yaml: %v", code, err)
	}
	keys := lf.Keys
	if keys == nil {
		keys = map[string]string{}
	}
	return Language{Code: lf.Code, Name: lf.Name, Native: lf.Native, Keys: keys}
}

func readBundledPreset(t *testing.T, preset string) []byte {
	t.Helper()
	raw, err := defaults.FS.ReadFile(filepath.ToSlash(filepath.Join("config", preset+".yaml")))
	if err != nil {
		t.Fatalf("read bundled %s.yaml: %v", preset, err)
	}
	return raw
}

func loadPresetGolden(t *testing.T, preset string) []string {
	t.Helper()
	path := filepath.Join("testdata", "preset_message_golden", preset+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	var messages []string
	if err := json.Unmarshal(raw, &messages); err != nil {
		t.Fatalf("decode golden %s: %v", path, err)
	}
	return messages
}
