package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHashFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "file.txt")
	if err := os.WriteFile(path, []byte("hello"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	hash, err := HashFile(path)
	if err != nil {
		t.Fatalf("HashFile() error = %v", err)
	}
	if hash == "" {
		t.Fatal("HashFile() returned empty hash")
	}

	_, err = HashFile(filepath.Join(tmp, "missing"))
	if err == nil {
		t.Fatal("HashFile(missing) error = nil")
	}
}

func TestLoadBundlePlanHashesCapturedWiringAndEntityBytes(t *testing.T) {
	tmp := t.TempDir()
	if err := EnsureDefaultFiles(tmp); err != nil {
		t.Fatalf("EnsureDefaultFiles() error = %v", err)
	}
	path := filepath.Join(tmp, "config", "omakase.yaml")
	bundle, hashes, err := LoadBundlePlan(path)
	if err != nil {
		t.Fatalf("LoadBundlePlan() error = %v", err)
	}
	paths := append([]string{path}, bundle.SourcePaths[1:]...)
	if len(bundle.Skills) > 0 {
		paths = append(paths, bundle.Skills[0].SourcePath)
	}
	for _, sourcePath := range paths {
		raw, err := os.ReadFile(sourcePath)
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v", sourcePath, err)
		}
		if got, want := hashes[sourcePath], hashBytes(raw); got != want {
			t.Fatalf("hash[%s] = %q, want %q from captured bytes", sourcePath, got, want)
		}
	}
	if len(bundle.Skills) > 0 {
		raw, err := os.ReadFile(bundle.Skills[0].SourcePath)
		if err != nil {
			t.Fatal(err)
		}
		_, body, err := SplitFrontmatter(raw)
		if err != nil {
			t.Fatal(err)
		}
		if bundle.Skills[0].Body != string(body) {
			t.Fatalf("planned skill body was not parsed from the captured source bytes")
		}
	}
}

func TestWriteAtomic(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "dir", "file.txt")

	if err := WriteAtomic(path, []byte("atomic content")); err != nil {
		t.Fatalf("WriteAtomic() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "atomic content" {
		t.Fatalf("ReadFile() = %q, want %q", string(data), "atomic content")
	}
}

func TestEnsureDefaultFiles(t *testing.T) {
	tmp := t.TempDir()
	if err := EnsureDefaultFiles(tmp); err != nil {
		t.Fatalf("EnsureDefaultFiles() error = %v", err)
	}
	assertDefaultLayout(t, tmp)
	if err := EnsureDefaultFiles(tmp); err != nil {
		t.Fatalf("EnsureDefaultFiles() second call error = %v", err)
	}
}

func assertDefaultLayout(t *testing.T, tmp string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(tmp, "config", "omakase.yaml")); err != nil {
		t.Fatalf("config/omakiten.yaml missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "config", "custom")); err != nil {
		t.Fatalf("config/custom dir missing: %v", err)
	}
	for _, name := range []string{"omakase.yaml"} {
		if _, err := os.Stat(filepath.Join(tmp, "config", name)); err != nil {
			t.Fatalf("default config profile %s missing: %v", name, err)
		}
	}

	for _, dir := range []string{"skills", "laws", "personas", "templates", "themes", "languages"} {
		if _, err := os.Stat(filepath.Join(tmp, dir)); err != nil {
			t.Fatalf("%s dir missing: %v", dir, err)
		}
		// Each entity dir must have a custom/ subtree the user can write into.
		if _, err := os.Stat(filepath.Join(tmp, dir, "custom")); err != nil {
			t.Fatalf("%s/custom dir missing: %v", dir, err)
		}
	}

	// The bundled English language pack must materialize so the catalog has
	// a baseline to fall back to even when the user has not chosen a language.
	if _, err := os.Stat(filepath.Join(tmp, "languages", "en.yaml")); err != nil {
		t.Fatalf("default language en.yaml missing: %v", err)
	}

	// The default kit must ship the task and PR templates; otherwise the embed.FS
	// pattern silently dropped the templates/ subtree.
	for _, name := range []string{"user-story.md", "pull-request.md"} {
		if _, err := os.Stat(filepath.Join(tmp, "templates", name)); err != nil {
			t.Fatalf("default template %s missing: %v", name, err)
		}
	}
}

func TestRefreshDefaultFilesOverwrites(t *testing.T) {
	tmp := t.TempDir()
	if err := EnsureDefaultFiles(tmp); err != nil {
		t.Fatalf("EnsureDefaultFiles() error = %v", err)
	}

	// Mutate a default file to simulate user customization at the root.
	defaultPath := filepath.Join(tmp, "skills", "implementation.md")
	if err := os.WriteFile(defaultPath, []byte("# user edited\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	customPath := filepath.Join(tmp, "skills", "custom", "mine.md")
	if err := os.WriteFile(customPath, []byte("---\nname: Mine\n---\nmine\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := RefreshDefaultFiles(tmp); err != nil {
		t.Fatalf("RefreshDefaultFiles() error = %v", err)
	}

	got, err := os.ReadFile(defaultPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) == "# user edited\n" {
		t.Fatalf("RefreshDefaultFiles did not overwrite the default file")
	}

	if _, err := os.Stat(customPath); err != nil {
		t.Fatalf("RefreshDefaultFiles touched custom/: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "config", "omakase.yaml")); err != nil {
		t.Fatalf("RefreshDefaultFiles did not keep default config profiles: %v", err)
	}
}

func TestLoadThemeMissing(t *testing.T) {
	_, err := LoadTheme(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatal("LoadTheme(missing) error = nil")
	}
}

type unsafeThemeReader struct {
	err error
}

func (r unsafeThemeReader) readFile(path string, _ int64) ([]byte, error) {
	if strings.Contains(filepath.ToSlash(path), "/custom/") {
		return nil, r.err
	}
	return []byte("version: 1\nkey: default\nname: Default\ncolors:\n  bg: '#000000'\n"), nil
}

func (unsafeThemeReader) listFiles(string, []string, bool, int64) ([]entityFile, error) {
	return nil, nil
}

func TestResolveActiveThemeDoesNotFallbackOnUnsafeCustomError(t *testing.T) {
	unsafeErr := errors.New("custom theme is unsafe")
	_, _, err := resolveActiveThemeReader(t.TempDir(), "dark", unsafeThemeReader{err: unsafeErr})
	if !errors.Is(err, unsafeErr) {
		t.Fatalf("resolveActiveThemeReader error = %v, want %v", err, unsafeErr)
	}
}

// TestLoadBundlePopulatesActiveTheme pins the new contract: LoadBundle
// resolves themes/<active>.yaml (custom→default precedence) during the
// bundle assembly so downstream consumers read tokens through
// Snapshot.Theme() instead of re-opening the YAML at every hot-reload.
// On the happy path the theme tokens are populated; on a missing or
// invalid theme the loader degrades to a zero-Theme + a SourceWarning so
// CLI commands that never render the TUI continue to work.
func TestLoadBundlePopulatesActiveTheme(t *testing.T) {
	tmp := t.TempDir()
	if err := EnsureDefaultFiles(tmp); err != nil {
		t.Fatalf("EnsureDefaultFiles() error = %v", err)
	}
	yamlPath := filepath.Join(tmp, "config", "omakase.yaml")
	assertActiveThemeLoads(t, tmp, yamlPath)
}

func assertActiveThemeLoads(t *testing.T, tmp, yamlPath string) {
	t.Helper()
	bundle, err := LoadBundle(yamlPath)
	if err != nil {
		t.Fatalf("LoadBundle(happy path) error = %v", err)
	}
	assertActiveThemeHappy(t, bundle)
	removeActiveThemeFiles(t, tmp, bundle.Config.Theme.Active)
	bundle, err = LoadBundle(yamlPath)
	if err != nil {
		t.Fatalf("LoadBundle(theme missing) error = %v, want nil (degraded path)", err)
	}
	assertMissingActiveTheme(t, bundle)
}

func assertActiveThemeHappy(t *testing.T, bundle Bundle) {
	t.Helper()
	if bundle.ActiveTheme.Name == "" {
		t.Fatalf("LoadBundle: ActiveTheme.Name = \"\", want populated from themes/<active>.yaml")
	}
	if bundle.ActiveThemeErr != nil {
		t.Fatalf("LoadBundle(happy path): ActiveThemeErr = %v, want nil", bundle.ActiveThemeErr)
	}
	if bundle.Config.Theme.Active == "" {
		t.Fatal("test fixture issue: bundle.Config.Theme.Active is empty; precondition for the case")
	}

	for _, w := range bundle.Warnings {
		if w.Path != "" && filepath.Base(filepath.Dir(w.Path)) == "themes" {
			t.Fatalf("LoadBundle(happy path): unexpected theme warning %+v", w)
		}
	}
}

func removeActiveThemeFiles(t *testing.T, tmp, active string) {
	t.Helper()
	for _, candidate := range []string{
		filepath.Join(tmp, "themes", "custom", active+".yaml"),
		filepath.Join(tmp, "themes", active+".yaml"),
	} {
		_ = os.Remove(candidate)
	}
}

func assertMissingActiveTheme(t *testing.T, bundle Bundle) {
	t.Helper()
	if bundle.ActiveTheme.Name != "" {
		t.Fatalf("LoadBundle(theme missing): ActiveTheme = %+v, want zero-Theme", bundle.ActiveTheme)
	}
	if bundle.ActiveThemeErr == nil {
		t.Fatal("LoadBundle(theme missing): ActiveThemeErr = nil, want non-nil")
	}
	if !errors.Is(bundle.ActiveThemeErr, os.ErrNotExist) {
		t.Fatalf("LoadBundle(theme missing): ActiveThemeErr = %v, want wraps os.ErrNotExist", bundle.ActiveThemeErr)
	}
	if msg := bundle.ActiveThemeErr.Error(); !strings.Contains(msg, "custom=") || !strings.Contains(msg, "default=") {
		t.Fatalf("LoadBundle(theme missing): ActiveThemeErr message %q does not name both candidate paths", msg)
	}
	var sawWarning bool
	for _, w := range bundle.Warnings {
		if filepath.Base(filepath.Dir(w.Path)) == "themes" || filepath.Base(filepath.Dir(filepath.Dir(w.Path))) == "themes" {
			sawWarning = true
			break
		}
	}
	if !sawWarning {
		t.Fatalf("LoadBundle(theme missing): no themes-scoped warning in %+v", bundle.Warnings)
	}
}
