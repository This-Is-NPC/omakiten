package cli

import (
	"os"
	"path/filepath"
	"testing"

	"omakiten/internal/config"
)

// writeBrokenConfig points $OMAKITEN_HOME at a fresh temp dir and writes
// a single broken profile carrying the given languages block. The bundle
// is rejected by ValidateBundle (bumped version, no kit/workflows) so the
// normal LoadBundle path fails — exactly the scenario the probe rescues.
func writeBrokenConfig(t *testing.T, languages string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("OMAKITEN_HOME", home)
	cfgDir := filepath.Join(home, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	body := "version: 99\nconfig:\n" + languages
	if err := os.WriteFile(filepath.Join(cfgDir, "omakase.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write broken config: %v", err)
	}
}

func enBaseline(t *testing.T) *config.Language {
	t.Helper()
	baseline, err := config.LoadBundledLanguage("en")
	if err != nil {
		t.Fatalf("LoadBundledLanguage(en): %v", err)
	}
	return &baseline
}

// TestBootstrapActiveLanguage_BrokenBundleUsesProbe is the core #370
// behaviour: a bundle that ValidateBundle rejects still yields a
// localised catalog because the probe reads languages.cli from the
// otherwise-broken file and resolves it through the embed FS.
func TestBootstrapActiveLanguage_BrokenBundleUsesProbe(t *testing.T) {
	writeBrokenConfig(t, "  languages:\n    cli: pt-br\n    tui: pt-br\n")
	baseline := enBaseline(t)

	got := bootstrapActiveLanguage(config.SurfaceCLI, baseline)
	if got.Code != "pt-br" {
		t.Fatalf("CLI surface: active code = %q, want pt-br (probe should win over baseline)", got.Code)
	}

	gotTUI := bootstrapActiveLanguage(config.SurfaceTUI, baseline)
	if gotTUI.Code != "pt-br" {
		t.Fatalf("TUI surface: active code = %q, want pt-br", gotTUI.Code)
	}
}

// TestBootstrapActiveLanguage_SurfaceCodesAreIndependent pins that the
// probe honours the per-surface code: a broken bundle with cli=pt-br but
// tui empty resolves pt-br for CLI and falls back to baseline for TUI.
func TestBootstrapActiveLanguage_SurfaceCodesAreIndependent(t *testing.T) {
	writeBrokenConfig(t, "  languages:\n    cli: pt-br\n")
	baseline := enBaseline(t)

	if got := bootstrapActiveLanguage(config.SurfaceCLI, baseline); got.Code != "pt-br" {
		t.Fatalf("CLI surface: active code = %q, want pt-br", got.Code)
	}
	// TUI code empty in the broken bundle and LoadBundle fails → baseline.
	if got := bootstrapActiveLanguage(config.SurfaceTUI, baseline); got.Code != baseline.Code {
		t.Fatalf("TUI surface: active code = %q, want baseline %q", got.Code, baseline.Code)
	}
}

// TestBootstrapActiveLanguage_EmptyProbeFallsThroughToLoadBundle pins
// that an empty languages block does not short-circuit resolution: the
// probe returns ok with an empty code, so the existing LoadBundle path
// runs. On a healthy default bundle that yields the en baseline.
func TestBootstrapActiveLanguage_EmptyProbeFallsThroughToLoadBundle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMAKITEN_HOME", home)
	if err := config.EnsureDefaultFiles(home); err != nil {
		t.Fatalf("EnsureDefaultFiles: %v", err)
	}
	baseline := enBaseline(t)

	// Default bundle ships no explicit languages block → probe code empty
	// → LoadBundle path runs and resolves the en baseline cleanly.
	got := bootstrapActiveLanguage(config.SurfaceCLI, baseline)
	if got.Code != baseline.Code {
		t.Fatalf("active code = %q, want baseline %q via LoadBundle path", got.Code, baseline.Code)
	}
}

// TestBootstrapActiveLanguage_CustomPackRequiresLoadBundle pins the
// documented limitation: a broken bundle naming a code the embed FS
// cannot resolve (a hypothetical custom pack) falls through — the probe
// found a code but LoadBundledLanguage fails, and the broken bundle
// can't deliver the pack either, so the user lands on the baseline.
func TestBootstrapActiveLanguage_CustomPackRequiresLoadBundle(t *testing.T) {
	writeBrokenConfig(t, "  languages:\n    cli: xx-custom\n")
	baseline := enBaseline(t)

	got := bootstrapActiveLanguage(config.SurfaceCLI, baseline)
	if got.Code != baseline.Code {
		t.Fatalf("active code = %q, want baseline %q (custom pack unresolvable on broken bundle)", got.Code, baseline.Code)
	}
}
