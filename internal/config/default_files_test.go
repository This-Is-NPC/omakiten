package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/paths"
)

func TestRefreshDefaultFilesPrunesManagedAndPreservesUserState(t *testing.T) {
	fixture := seedRefreshDefaultFiles(t)

	if err := RefreshDefaultFiles(fixture.root); err != nil {
		t.Fatalf("RefreshDefaultFiles: %v", err)
	}
	assertRefreshPreservedState(t, fixture)
}

type refreshDefaultFixture struct {
	root, activePath, profilePath string
	activeBytes                   []byte
	customFiles                   map[string]string
	staleManaged                  []string
}

func seedRefreshDefaultFiles(t *testing.T) refreshDefaultFixture {
	t.Helper()
	root := t.TempDir()
	if err := EnsureDefaultFiles(root); err != nil {
		t.Fatalf("EnsureDefaultFiles: %v", err)
	}
	fixture := refreshDefaultFixture{
		root:        root,
		activePath:  filepath.Join(root, "config", paths.ActiveConfigStateFile),
		activeBytes: []byte("kaiseki.yaml\n"),
		customFiles: map[string]string{
			filepath.Join("config", "custom", "user.yaml"):          "user config\n",
			filepath.Join("skills", "custom", "user.md"):            "user skill\n",
			filepath.Join("laws", "custom", "user.md"):              "user law\n",
			filepath.Join("personas", "custom", "user.md"):          "user persona\n",
			filepath.Join("templates", "custom", "user.md"):         "user template\n",
			filepath.Join("themes", "custom", "user.yaml"):          "user theme\n",
			filepath.Join("notifications", "custom", "user.yaml"):   "user notification\n",
			filepath.Join("languages", "custom", "user.yaml"):       "user language\n",
			filepath.Join("languages", "custom", "nested", "x.txt"): "nested custom\n",
		},
		staleManaged: []string{
			filepath.Join(root, "config", "stale.yaml"),
			filepath.Join(root, "config", "modules", "stale.yaml"),
			filepath.Join(root, "skills", "stale.md"),
			filepath.Join(root, "skills", "nested", "stale.md"),
			filepath.Join(root, "languages", "stale.yaml"),
		},
	}
	if err := os.WriteFile(fixture.activePath, fixture.activeBytes, 0o644); err != nil {
		t.Fatalf("seed .active: %v", err)
	}
	for rel, body := range fixture.customFiles {
		writeConfigTestFile(t, filepath.Join(root, rel), body)
	}
	for _, path := range fixture.staleManaged {
		writeConfigTestFile(t, path, "stale\n")
	}
	fixture.profilePath = filepath.Join(root, "config", "omakase.yaml")
	writeConfigTestFile(t, fixture.profilePath, "version: 1\n# flattened stale copy\n")
	return fixture
}

func assertRefreshPreservedState(t *testing.T, fixture refreshDefaultFixture) {
	t.Helper()
	if got, err := os.ReadFile(fixture.activePath); err != nil || string(got) != string(fixture.activeBytes) {
		t.Fatalf(".active after refresh = %q, %v; want %q", got, err, fixture.activeBytes)
	}
	for rel, want := range fixture.customFiles {
		got, err := os.ReadFile(filepath.Join(fixture.root, rel))
		if err != nil {
			t.Fatalf("custom file %s missing after refresh: %v", rel, err)
		}
		if string(got) != want {
			t.Fatalf("custom file %s = %q, want %q", rel, got, want)
		}
	}
	for _, path := range fixture.staleManaged {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("stale managed path %s survived refresh; err=%v", path, err)
		}
	}

	refreshedProfile, err := os.ReadFile(fixture.profilePath)
	if err != nil {
		t.Fatalf("read refreshed profile: %v", err)
	}
	// The pre-refresh profile (seeded above) carried a distinctive
	// "# flattened stale copy" marker. Its absence proves the refresh
	// actually replaced the file from the embedded defaults rather than
	// no-op'ing over a stale copy — without this, a refresh that never
	// overwrote the profile would still pass the import-form assertions
	// below if the stale copy happened to contain the same imports.
	if got := string(refreshedProfile); strings.Contains(got, "# flattened stale copy") {
		t.Fatalf("refreshed profile still carries the seeded stale marker; file was not overwritten:\n%s", got[:min(len(got), 400)])
	}
	if got := string(refreshedProfile); !strings.Contains(got, "merge_from: ./modules/base-config.yaml") || !strings.Contains(got, "from: ./themes/naruto.yaml#personas") || !strings.Contains(got, "from: ./modules/surfaces.yaml") {
		t.Fatalf("refreshed profile lost shipped import form:\n%s", got[:min(len(got), 400)])
	}
	if _, err := os.Stat(filepath.Join(fixture.root, "config", "modules", "base-config.yaml")); err != nil {
		t.Fatalf("shipped config module not restored: %v", err)
	}
}

func TestRefreshDefaultFilesRefusesTopLevelManagedSymlink(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	victim := filepath.Join(external, "victim.md")
	writeConfigTestFile(t, victim, "keep me\n")
	if err := os.Symlink(external, filepath.Join(root, "skills")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	if err := RefreshDefaultFiles(root); err == nil {
		t.Fatalf("RefreshDefaultFiles accepted a top-level managed symlink; want refusal")
	}
	if got, err := os.ReadFile(victim); err != nil || string(got) != "keep me\n" {
		t.Fatalf("external victim after refused refresh = %q, %v; want untouched", got, err)
	}
}

func TestRefreshDefaultFilesPreservesCustomSymlink(t *testing.T) {
	root := t.TempDir()
	if err := EnsureDefaultFiles(root); err != nil {
		t.Fatalf("EnsureDefaultFiles: %v", err)
	}
	externalCustom := t.TempDir()
	customFile := filepath.Join(externalCustom, "user.md")
	writeConfigTestFile(t, customFile, "custom body\n")
	customLink := filepath.Join(root, "skills", "custom")
	if err := os.RemoveAll(customLink); err != nil {
		t.Fatalf("remove seeded custom dir: %v", err)
	}
	if err := os.Symlink(externalCustom, customLink); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	if err := RefreshDefaultFiles(root); err != nil {
		t.Fatalf("RefreshDefaultFiles: %v", err)
	}
	info, err := os.Lstat(customLink)
	if err != nil {
		t.Fatalf("lstat custom link: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("custom link mode = %v, want symlink preserved", info.Mode())
	}
	if got, err := os.ReadFile(customFile); err != nil || string(got) != "custom body\n" {
		t.Fatalf("custom target after refresh = %q, %v; want untouched", got, err)
	}
}

func writeConfigTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
