package config

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/defaults"
)

// TestNewLanguagePackScript exercises scripts/new-language-pack.sh against the
// real defaults/languages/en.yaml baseline. It writes a throwaway pack under
// a code unlikely to collide with any bundled or future BCP-47 selection,
// then asserts:
//   - the destination decodes via the same strict loader the runtime uses,
//   - the header swap landed (code, name, native),
//   - every translated value carries a `# TODO(translate): <key>` comment.
// The pack is removed at end-of-test so the parity suite is unaffected.
func TestNewLanguagePackScript(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH; scaffold script requires bash")
	}

	repoRoot := findRepoRoot(t)
	script := filepath.Join(repoRoot, "scripts", "new-language-pack.sh")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("scaffold script missing: %v", err)
	}

	const (
		code   = "zz-test"
		native = "Zzznative"
		name   = "Zztest"
	)
	dst := filepath.Join(repoRoot, "defaults", "languages", code+".yaml")
	if _, err := os.Stat(dst); err == nil {
		t.Fatalf("test artifact %s already exists; refusing to clobber", dst)
	}
	t.Cleanup(func() { _ = os.Remove(dst) })

	cmd := exec.Command("bash", script, code, native, name)
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("scaffold run: %v\n%s", err, out)
	}

	raw, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read scaffolded pack: %v", err)
	}

	var lf languageFile
	if err := decodeLanguageStrict(raw, &lf); err != nil {
		t.Fatalf("scaffolded pack failed strict decode: %v", err)
	}
	if lf.Code != code {
		t.Errorf("code: got %q want %q", lf.Code, code)
	}
	if lf.Name != name {
		t.Errorf("name: got %q want %q", lf.Name, name)
	}
	if lf.Native != native {
		t.Errorf("native: got %q want %q", lf.Native, native)
	}

	en := loadBundledLanguage(t, "en")
	if len(lf.Keys) != len(en.Keys) {
		t.Errorf("scaffold key count %d differs from en baseline %d", len(lf.Keys), len(en.Keys))
	}

	markers := collectTodoMarkers(t, dst)
	for key := range en.Keys {
		if _, ok := markers[key]; !ok {
			t.Errorf("scaffolded pack missing `# TODO(translate): %s` marker", key)
		}
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repo root (go.mod) not found above %s", wd)
		}
		dir = parent
	}
}

func collectTodoMarkers(t *testing.T, path string) map[string]struct{} {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open scaffolded pack: %v", err)
	}
	defer f.Close()

	const prefix = "# TODO(translate): "
	out := map[string]struct{}{}
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if rest, ok := strings.CutPrefix(line, prefix); ok {
			out[rest] = struct{}{}
		}
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan scaffolded pack: %v", err)
	}
	return out
}

// silence unused import warning if defaults pkg drops out during refactors.
var _ = defaults.FS
