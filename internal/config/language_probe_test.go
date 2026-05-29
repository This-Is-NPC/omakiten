package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// brokenBundleYAML is a profile that parses as YAML at the top level and
// carries a valid config.languages block, but is rejected by
// ValidateBundle (bumped version, no kit/workflows/required settings).
// It is the canonical "broken config" shape the probe must survive.
const brokenBundleYAML = `version: 99
config:
  languages:
    cli: pt-br
    tui: pt-br
`

func writeTempYAML(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "omakase.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write temp yaml: %v", err)
	}
	return path
}

func TestProbeLanguageSetting_ParsesValidYAML(t *testing.T) {
	path := writeTempYAML(t, `version: 1
config:
  languages:
    cli: pt-br
    tui: es
    agent_output: Português
`)
	got, ok := ProbeLanguageSetting(path)
	if !ok {
		t.Fatal("ProbeLanguageSetting ok = false, want true")
	}
	want := LanguageSettings{CLI: "pt-br", TUI: "es", AgentOutput: "Português"}
	if got != want {
		t.Fatalf("ProbeLanguageSetting = %+v, want %+v", got, want)
	}
}

func TestProbeLanguageSetting_SurvivesValidateBundleReject(t *testing.T) {
	path := writeTempYAML(t, brokenBundleYAML)

	// LoadBundle must reject this bundle...
	if _, err := LoadBundle(path); err == nil {
		t.Fatal("LoadBundle on broken bundle: err = nil, want non-nil")
	}
	// ...but the probe still recovers the configured codes.
	got, ok := ProbeLanguageSetting(path)
	if !ok {
		t.Fatal("ProbeLanguageSetting ok = false on broken bundle, want true")
	}
	if got.CLI != "pt-br" || got.TUI != "pt-br" {
		t.Fatalf("ProbeLanguageSetting = %+v, want cli/tui = pt-br", got)
	}
}

func TestProbeLanguageSetting_HandlesMissingLanguagesBlock(t *testing.T) {
	path := writeTempYAML(t, `version: 1
config:
  output:
    format: text
`)
	got, ok := ProbeLanguageSetting(path)
	if !ok {
		t.Fatal("ProbeLanguageSetting ok = false, want true on a parseable file")
	}
	if (got != LanguageSettings{}) {
		t.Fatalf("ProbeLanguageSetting = %+v, want zero LanguageSettings", got)
	}
}

func TestProbeLanguageSetting_HandlesIOError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.yaml")
	if _, ok := ProbeLanguageSetting(path); ok {
		t.Fatal("ProbeLanguageSetting ok = true on missing file, want false")
	}
}

func TestProbeLanguageSetting_HandlesMalformedYAML(t *testing.T) {
	path := writeTempYAML(t, "config:\n  languages: : : not yaml\n   bad indent\n")
	if _, ok := ProbeLanguageSetting(path); ok {
		t.Fatal("ProbeLanguageSetting ok = true on malformed yaml, want false")
	}
}

// TestProbeLanguageSetting_MatchesLoadBundleOnHealthyBundle pins the
// wrapper-tag shape (config: → languages:) against a real healthy
// bundle: the probe must read the identical languages block LoadBundle
// surfaces on its raw Config.Languages. A drift in either wrapper tag
// would make these diverge.
func TestProbeLanguageSetting_MatchesLoadBundleOnHealthyBundle(t *testing.T) {
	tmp := t.TempDir()
	if err := EnsureDefaultFiles(tmp); err != nil {
		t.Fatalf("EnsureDefaultFiles: %v", err)
	}
	path := filepath.Join(tmp, "config", "omakase.yaml")

	bundle, err := LoadBundle(path)
	if err != nil {
		t.Fatalf("LoadBundle(healthy): %v", err)
	}
	got, ok := ProbeLanguageSetting(path)
	if !ok {
		t.Fatal("ProbeLanguageSetting ok = false on healthy bundle, want true")
	}
	if got != bundle.Config.Languages {
		t.Fatalf("probe = %+v, LoadBundle Config.Languages = %+v", got, bundle.Config.Languages)
	}
}

// TestProbeLanguageSettingMatchesLanguageSettingsTagShape is the
// compile-time-plus-reflection guard for AC 2: the leaf field tags the
// probe relies on live only on LanguageSettings. If CLI/TUI are renamed
// or their yaml tags change, this fails — flagging that the probe's
// wrapper (which embeds LanguageSettings by name) must be re-verified.
func TestProbeLanguageSettingMatchesLanguageSettingsTagShape(t *testing.T) {
	typ := reflect.TypeOf(LanguageSettings{})
	for field, wantTag := range map[string]string{"CLI": "cli", "TUI": "tui"} {
		f, ok := typ.FieldByName(field)
		if !ok {
			t.Fatalf("LanguageSettings.%s missing — probe wrapper relies on it", field)
		}
		if got := f.Tag.Get("yaml"); got != wantTag+",omitempty" {
			t.Fatalf("LanguageSettings.%s yaml tag = %q, want %q", field, got, wantTag+",omitempty")
		}
	}
}
