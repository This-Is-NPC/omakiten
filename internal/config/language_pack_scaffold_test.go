package config

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewLanguagePackScript(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH; scaffold script requires bash")
	}

	workspace := scaffoldWorkspace(t)
	script := filepath.Join(workspace, "scripts", "new-language-pack.sh")
	t.Setenv("MISE_PROJECT_ROOT", workspace)

	const (
		code   = "zz-test"
		native = "Native: \"quoted\" # name"
		name   = "English: \"quoted\" # name"
	)
	dst := filepath.Join(workspace, "defaults", "languages", code+".yaml")

	cmd := exec.Command("bash", script, code, native, name)
	cmd.Dir = workspace
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("scaffold run: %v\n%s", err, out)
	}

	raw, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read scaffolded pack: %v", err)
	}

	var lf languageFile
	if err := decodeLanguageStrict(raw, &lf); err != nil {
		t.Fatalf("scaffolded pack failed strict decode: %v\n%s", err, raw[:min(len(raw), 600)])
	}
	if lf.Code != code || lf.Name != name || lf.Native != native {
		t.Fatalf("header = %q %q %q, want %q %q %q", lf.Code, lf.Name, lf.Native, code, name, native)
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
	cmd = exec.Command("bash", script, code, "different native", "different name")
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("scaffold overwrote an existing pack: %s", out)
	}
	unchanged, err := os.ReadFile(dst)
	if err != nil || string(unchanged) != string(raw) {
		t.Fatalf("existing pack changed after rejected overwrite: %v", err)
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

func scaffoldWorkspace(t *testing.T) string {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	for _, relative := range []string{"scripts/new-language-pack.sh", "scripts/new-language-pack.go", "scripts/lib/workspace.sh", "defaults/languages/en.yaml"} {
		data, err := os.ReadFile(filepath.Join(repoRoot, relative))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(workspace, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return workspace
}
