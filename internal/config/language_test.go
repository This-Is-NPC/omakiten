package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLanguageFile(t *testing.T, dir, code, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, code+".yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func langDirWith(t *testing.T, bundled, custom map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for code, body := range bundled {
		writeLanguageFile(t, dir, code, body)
	}
	for code, body := range custom {
		writeLanguageFile(t, filepath.Join(dir, "custom"), code, body)
	}
	return dir
}

func TestLoadLanguages_singleBundled(t *testing.T) {
	dir := langDirWith(t, map[string]string{
		"en": `code: en
name: English
native: English
keys:
  cli.hello: Hello
  tui.bye: Bye
`,
	}, nil)
	langs, warns, err := LoadLanguages(dir)
	if err != nil {
		t.Fatalf("LoadLanguages: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("expected no warnings, got %v", warns)
	}
	if len(langs) != 1 {
		t.Fatalf("got %d languages, want 1", len(langs))
	}
	got := langs[0]
	if got.Code != "en" || got.Name != "English" || got.Native != "English" {
		t.Fatalf("metadata mismatch: %+v", got)
	}
	if got.Keys["cli.hello"] != "Hello" || got.Keys["tui.bye"] != "Bye" {
		t.Fatalf("keys mismatch: %+v", got.Keys)
	}
	if got.IsCustom {
		t.Fatalf("bundled language flagged as custom")
	}
}

func TestLoadLanguages_customOverridesBundled(t *testing.T) {
	dir := langDirWith(t,
		map[string]string{
			"en": `code: en
name: English
native: English
keys:
  cli.hello: Hello
`,
		},
		map[string]string{
			"en": `code: en
name: English
native: English
keys:
  cli.hello: Howdy
  cli.extra: Extra
`,
		},
	)
	langs, _, err := LoadLanguages(dir)
	if err != nil {
		t.Fatalf("LoadLanguages: %v", err)
	}
	if len(langs) != 1 {
		t.Fatalf("got %d, want 1", len(langs))
	}
	got := langs[0]
	if !got.IsCustom {
		t.Fatalf("expected custom to win, got bundled")
	}
	if got.Keys["cli.hello"] != "Howdy" {
		t.Fatalf("expected custom override Howdy, got %q", got.Keys["cli.hello"])
	}
	if got.Keys["cli.extra"] != "Extra" {
		t.Fatalf("expected custom-only key, got %q", got.Keys["cli.extra"])
	}
}

func TestLoadLanguages_emptyKeysAllowed(t *testing.T) {
	dir := langDirWith(t, map[string]string{
		"en": `code: en
name: English
native: English
`,
	}, nil)
	langs, _, err := LoadLanguages(dir)
	if err != nil {
		t.Fatalf("LoadLanguages: %v", err)
	}
	if len(langs) != 1 {
		t.Fatalf("got %d, want 1", len(langs))
	}
	if langs[0].Keys == nil {
		t.Fatalf("expected non-nil empty Keys map, got nil")
	}
	if len(langs[0].Keys) != 0 {
		t.Fatalf("expected empty Keys, got %d entries", len(langs[0].Keys))
	}
}

func TestLoadLanguages_missingCodeRejected(t *testing.T) {
	dir := langDirWith(t, map[string]string{
		"en": `name: English
native: English
`,
	}, nil)
	_, _, err := LoadLanguages(dir)
	if err == nil || !strings.Contains(err.Error(), "code") {
		t.Fatalf("expected error about missing code, got %v", err)
	}
}

func TestLoadLanguages_missingNameRejected(t *testing.T) {
	dir := langDirWith(t, map[string]string{
		"en": `code: en
native: English
`,
	}, nil)
	_, _, err := LoadLanguages(dir)
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("expected error about missing name, got %v", err)
	}
}

func TestLoadLanguages_missingNativeRejected(t *testing.T) {
	dir := langDirWith(t, map[string]string{
		"en": `code: en
name: English
`,
	}, nil)
	_, _, err := LoadLanguages(dir)
	if err == nil || !strings.Contains(err.Error(), "native") {
		t.Fatalf("expected error about missing native, got %v", err)
	}
}

func TestLoadLanguages_codeMustBeLowercase(t *testing.T) {
	dir := langDirWith(t, map[string]string{
		"en": `code: EN
name: English
native: English
`,
	}, nil)
	_, _, err := LoadLanguages(dir)
	if err == nil || !strings.Contains(err.Error(), "lowercase") {
		t.Fatalf("expected lowercase error, got %v", err)
	}
}

func TestLoadLanguages_codeMustMatchFilename(t *testing.T) {
	dir := langDirWith(t, map[string]string{
		"en": `code: pt-br
name: Portuguese
native: Português
`,
	}, nil)
	_, warns, err := LoadLanguages(dir)
	if err != nil {
		t.Fatalf("LoadLanguages: %v", err)
	}
	if len(warns) == 0 {
		t.Fatalf("expected mismatch warning, got none")
	}
	if !strings.Contains(warns[0].Message, "pt-br") || !strings.Contains(warns[0].Message, "en") {
		t.Fatalf("warning message lacks codes: %q", warns[0].Message)
	}
}

func TestLoadLanguages_duplicateInSameScopeRejected(t *testing.T) {
	dir := t.TempDir()
	writeLanguageFile(t, dir, "en", `code: en
name: English
native: English
`)
	// Manually create second file in same scope with different filename but same code.
	if err := os.WriteFile(filepath.Join(dir, "en-us.yaml"), []byte(`code: en
name: American
native: American
`), 0o644); err != nil {
		t.Fatalf("write second file: %v", err)
	}
	_, _, err := LoadLanguages(dir)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestLoadLanguages_missingDirReturnsEmpty(t *testing.T) {
	langs, warns, err := LoadLanguages(filepath.Join(t.TempDir(), "nonexistent"))
	if err != nil {
		t.Fatalf("missing dir should not error: %v", err)
	}
	if len(langs) != 0 || len(warns) != 0 {
		t.Fatalf("expected empty results, got %d langs / %d warns", len(langs), len(warns))
	}
}

func TestLoadLanguages_strictRejectsUnknownField(t *testing.T) {
	dir := langDirWith(t, map[string]string{
		"en": `code: en
name: English
native: English
extra: should-not-be-allowed
`,
	}, nil)
	_, _, err := LoadLanguages(dir)
	if err == nil {
		t.Fatalf("expected error on unknown field")
	}
}

func TestLoadLanguages_multipleBundledSortedByCode(t *testing.T) {
	dir := langDirWith(t, map[string]string{
		"pt-br": `code: pt-br
name: Portuguese (Brazil)
native: Português (Brasil)
keys: {}
`,
		"en": `code: en
name: English
native: English
keys: {}
`,
	}, nil)
	langs, _, err := LoadLanguages(dir)
	if err != nil {
		t.Fatalf("LoadLanguages: %v", err)
	}
	if len(langs) != 2 {
		t.Fatalf("got %d, want 2", len(langs))
	}
	if langs[0].Code != "en" || langs[1].Code != "pt-br" {
		t.Fatalf("expected stable order [en, pt-br], got [%s, %s]", langs[0].Code, langs[1].Code)
	}
}

func TestLoadLanguages_ignoresNonYAMLFiles(t *testing.T) {
	dir := langDirWith(t, map[string]string{
		"en": `code: en
name: English
native: English
`,
	}, nil)
	if err := os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# Languages"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("notes"), 0o644); err != nil {
		t.Fatalf("write notes: %v", err)
	}
	langs, _, err := LoadLanguages(dir)
	if err != nil {
		t.Fatalf("LoadLanguages: %v", err)
	}
	if len(langs) != 1 {
		t.Fatalf("got %d, want 1 (non-yaml ignored)", len(langs))
	}
}
