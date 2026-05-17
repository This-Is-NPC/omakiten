package config

import (
	"strings"
	"testing"
)

func bundleWithLanguages(langs []Language, settings LanguageSettings) Bundle {
	return Bundle{
		Languages: langs,
		Config:    Settings{Languages: settings},
	}
}

func TestSnapshot_Languages_emptyByDefault(t *testing.T) {
	snap := BuildSnapshot(Bundle{})
	if got := snap.Languages(); len(got) != 0 {
		t.Fatalf("Languages: got %d, want 0", len(got))
	}
	if got := snap.AgentOutputLanguage(); got != "" {
		t.Fatalf("AgentOutputLanguage: got %q, want empty", got)
	}
}

func TestSnapshot_Languages_returnsFreshCopy(t *testing.T) {
	en := Language{Code: "en", Name: "English", Native: "English", Keys: map[string]string{"k": "v"}}
	snap := BuildSnapshot(bundleWithLanguages([]Language{en}, LanguageSettings{}))
	first := snap.Languages()
	first[0].Code = "mutated"
	second := snap.Languages()
	if second[0].Code != "en" {
		t.Fatalf("snapshot mutation leaked: got %q", second[0].Code)
	}
}

func TestSnapshot_LanguageByCode(t *testing.T) {
	en := Language{Code: "en", Name: "English", Native: "English"}
	snap := BuildSnapshot(bundleWithLanguages([]Language{en}, LanguageSettings{}))
	got, ok := snap.LanguageByCode("en")
	if !ok || got.Name != "English" {
		t.Fatalf("LanguageByCode(en): %+v ok=%v", got, ok)
	}
	if _, ok := snap.LanguageByCode("xx"); ok {
		t.Fatalf("LanguageByCode(xx) returned ok=true")
	}
}

func TestSnapshot_CatalogResolvesActive(t *testing.T) {
	en := Language{Code: "en", Name: "English", Native: "English", Keys: map[string]string{"cli.hi": "Hello"}}
	ptbr := Language{Code: "pt-br", Name: "Portuguese", Native: "Português", Keys: map[string]string{"cli.hi": "Olá"}}
	snap := BuildSnapshot(bundleWithLanguages([]Language{en, ptbr}, LanguageSettings{CLI: "pt-br", TUI: "en"}))
	if got := snap.Catalog(SurfaceCLI).Get("cli.hi"); got != "Olá" {
		t.Fatalf("CLI catalog active: got %q, want Olá", got)
	}
	if got := snap.Catalog(SurfaceTUI).Get("cli.hi"); got != "Hello" {
		t.Fatalf("TUI catalog active: got %q, want Hello", got)
	}
}

func TestSnapshot_CatalogFallsBackToBaseline(t *testing.T) {
	en := Language{Code: "en", Name: "English", Native: "English", Keys: map[string]string{"cli.only_en": "fallback"}}
	ptbr := Language{Code: "pt-br", Name: "Portuguese", Native: "Português", Keys: map[string]string{}}
	snap := BuildSnapshot(bundleWithLanguages([]Language{en, ptbr}, LanguageSettings{CLI: "pt-br"}))
	if got := snap.Catalog(SurfaceCLI).Get("cli.only_en"); got != "fallback" {
		t.Fatalf("expected en baseline fallback, got %q", got)
	}
}

func TestSnapshot_CatalogUnknownConfiguredCodeFallsBackToBaseline(t *testing.T) {
	en := Language{Code: "en", Name: "English", Native: "English", Keys: map[string]string{"cli.hi": "Hello"}}
	snap := BuildSnapshot(bundleWithLanguages([]Language{en}, LanguageSettings{CLI: "xx"}))
	if got := snap.Catalog(SurfaceCLI).Get("cli.hi"); got != "Hello" {
		t.Fatalf("unknown configured CLI code should fall back to baseline, got %q", got)
	}
}

func TestSnapshot_CatalogMissingBaselineReturnsKey(t *testing.T) {
	snap := BuildSnapshot(bundleWithLanguages(nil, LanguageSettings{}))
	if got := snap.Catalog(SurfaceCLI).Get("cli.hi"); got != "cli.hi" {
		t.Fatalf("missing baseline + no active should return key literal, got %q", got)
	}
}

func TestSnapshot_AgentOutputLanguageRawString(t *testing.T) {
	en := Language{Code: "en", Name: "English", Native: "English"}
	snap := BuildSnapshot(bundleWithLanguages([]Language{en}, LanguageSettings{AgentOutput: "Português (Brasil)"}))
	if got := snap.AgentOutputLanguage(); got != "Português (Brasil)" {
		t.Fatalf("AgentOutputLanguage: got %q", got)
	}
}

func TestSnapshot_AgentOutputLanguageAcceptsNonCatalogValue(t *testing.T) {
	en := Language{Code: "en", Name: "English", Native: "English"}
	snap := BuildSnapshot(bundleWithLanguages([]Language{en}, LanguageSettings{AgentOutput: "Esperanto"}))
	if got := snap.AgentOutputLanguage(); got != "Esperanto" {
		t.Fatalf("free-form agent_output should pass through verbatim, got %q", got)
	}
}

func TestValidateLanguageSettings_acceptsKnownCodes(t *testing.T) {
	loaded := []Language{
		{Code: "en", Name: "English", Native: "English"},
		{Code: "pt-br", Name: "Portuguese", Native: "Português"},
	}
	cases := []LanguageSettings{
		{CLI: "en", TUI: "en"},
		{CLI: "pt-br", TUI: "en"},
		{CLI: "en", TUI: "pt-br", AgentOutput: "English"},
		{CLI: "en", TUI: "en", AgentOutput: "Esperanto (not loaded)"},
	}
	for _, ls := range cases {
		if err := validateLanguageSettings(ls, loaded); err != nil {
			t.Fatalf("validateLanguageSettings(%+v): %v", ls, err)
		}
	}
}

func TestValidateLanguageSettings_rejectsUnknownCLI(t *testing.T) {
	loaded := []Language{{Code: "en", Name: "English", Native: "English"}}
	err := validateLanguageSettings(LanguageSettings{CLI: "xx", TUI: "en"}, loaded)
	if err == nil || !strings.Contains(err.Error(), "languages.cli") || !strings.Contains(err.Error(), "xx") {
		t.Fatalf("expected cli rejection, got %v", err)
	}
	if !strings.Contains(err.Error(), "en") {
		t.Fatalf("error should list available codes, got %v", err)
	}
}

func TestValidateLanguageSettings_rejectsUnknownTUI(t *testing.T) {
	loaded := []Language{{Code: "en", Name: "English", Native: "English"}}
	err := validateLanguageSettings(LanguageSettings{CLI: "en", TUI: "xx"}, loaded)
	if err == nil || !strings.Contains(err.Error(), "languages.tui") {
		t.Fatalf("expected tui rejection, got %v", err)
	}
}

func TestValidateLanguageSettings_emptyDefaultsToEn(t *testing.T) {
	loaded := []Language{{Code: "en", Name: "English", Native: "English"}}
	if err := validateLanguageSettings(LanguageSettings{}, loaded); err != nil {
		t.Fatalf("empty settings with en loaded should pass, got %v", err)
	}
}

func TestValidateLanguageSettings_emptyLoadedSkipsValidation(t *testing.T) {
	// Legacy / test bundles with no languages folder bypass validation
	// entirely. Catalog still degrades gracefully (missing keys return
	// the key literal), so skipping here preserves existing behavior.
	if err := validateLanguageSettings(LanguageSettings{CLI: "xx", TUI: "yy"}, nil); err != nil {
		t.Fatalf("empty loaded should skip validation, got %v", err)
	}
}

func TestValidateLanguageSettings_agentOutputFreeForm(t *testing.T) {
	loaded := []Language{{Code: "en", Name: "English", Native: "English"}}
	for _, value := range []string{"English", "pt-br", "Português (Brasil)", "Esperanto", "Wookiee"} {
		err := validateLanguageSettings(LanguageSettings{CLI: "en", TUI: "en", AgentOutput: value}, loaded)
		if err != nil {
			t.Fatalf("agent_output %q should pass free-form validation, got %v", value, err)
		}
	}
}
