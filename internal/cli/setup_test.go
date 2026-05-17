package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func readBackContains(t *testing.T, path, want string) {
	t.Helper()
	got := readFile(t, path)
	if !strings.Contains(got, want) {
		t.Fatalf("%s missing %q\n--- contents ---\n%s", path, want, got)
	}
}

func readBackEquals(t *testing.T, path, want string) {
	t.Helper()
	got := readFile(t, path)
	if got != want {
		t.Fatalf("%s: got %q want %q", path, got, want)
	}
}

func unsetenv(name string) error { return os.Unsetenv(name) }

// TestCLISetupHeadless_FullEnvVars exercises the env-var-only headless
// path called out by task #119 AC §8: with the five OKT_* env vars
// supplied, `okt setup` runs without prompting and produces the
// expected on-disk side-effects (seeded preset, .active marker,
// omakiten.yaml languages block, optional rc wrapper).
//
// The harness pins both OMAKITEN_HOME (config root) and HOME (rc-file
// target) under tmpdirs so the test cannot stomp the user's real
// shell config — without those, WriteWrappers would touch ~/.bashrc.
func TestCLISetupHeadless_FullEnvVars(t *testing.T) {
	configHome := t.TempDir()
	rcHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "omakiten.db")
	// Seed rc files so WriteWrappers has somewhere to land. install.sh
	// only writes to existing files; replicate that to keep the test
	// faithful to the documented behaviour.
	for _, name := range []string{".bashrc", ".zshrc"} {
		writeFile(t, filepath.Join(rcHome, name), "# user content\n")
	}

	t.Setenv("OMAKITEN_HOME", configHome)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", rcHome)
	t.Setenv("OKT_CLI_LANG", "en")
	t.Setenv("OKT_TUI_LANG", "en")
	t.Setenv("OKT_AGENT_LANG", "Português (Brasil)")
	t.Setenv("OKT_PRESET", "omakase")
	t.Setenv("OKT_HARNESSES", "0") // skip sentinel — no harness exec needed
	t.Chdir(t.TempDir())

	out := runCLI(t, dbPath, "", "setup")
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)

	languages := data["languages"].(map[string]any)
	if languages["cli"] != "en" || languages["tui"] != "en" {
		t.Fatalf("languages: got %v, want cli=en tui=en", languages)
	}
	if languages["agent_output"] != "Português (Brasil)" {
		t.Fatalf("agent_output: got %v, want Português (Brasil)", languages["agent_output"])
	}

	preset := data["preset"].(map[string]any)
	if preset["name"] != "omakase" {
		t.Fatalf("preset name: got %v, want omakase", preset["name"])
	}

	wrapper := data["wrapper"].(map[string]any)
	installed := wrapper["installed_into"].([]any)
	if len(installed) != 2 {
		t.Fatalf("expected wrapper installed in 2 rc files, got %v", installed)
	}
	for _, rc := range installed {
		readBackContains(t, rc.(string), "# >>> okt wrapper >>>")
	}

	// .active marker reflects the chosen preset.
	readBackEquals(t, filepath.Join(configHome, "config", ".active"), "omakase.yaml\n")

	// omakiten.yaml persisted the languages block.
	readBackContains(t, filepath.Join(configHome, "config", "omakase.yaml"), "agent_output: Português (Brasil)")
}

// TestCLISetupHeadless_MissingEnvErrors guards the contract that the
// headless path refuses to half-run: without all five inputs, the
// caller gets a validation_error pointing at the missing knobs rather
// than a partially-applied install.
func TestCLISetupHeadless_MissingEnvErrors(t *testing.T) {
	configHome := t.TempDir()
	rcHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "omakiten.db")
	t.Setenv("OMAKITEN_HOME", configHome)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", rcHome)
	t.Setenv("OKT_CLI_LANG", "en")
	// TUI lang, agent lang, preset, harnesses deliberately omitted.
	t.Setenv("OKT_TUI_LANG", "")
	t.Setenv("OKT_PRESET", "")
	for _, env := range []string{"OKT_AGENT_LANG", "OKT_HARNESSES"} {
		if err := unsetenv(env); err != nil {
			t.Fatalf("unsetenv %s: %v", env, err)
		}
	}
	t.Chdir(t.TempDir())

	envelope := runCLIExpectError(t, dbPath, "", "validation_error", "setup")
	msg, _ := envelope["msg"].(string)
	if !strings.Contains(msg, "OKT_AGENT_LANG") || !strings.Contains(msg, "OKT_HARNESSES") {
		t.Fatalf("expected msg to name the missing env vars, got %q", msg)
	}
}

// TestCLISetupHeadless_SkipWrapper proves --skip-wrapper short-circuits
// the rc-file mutation entirely — useful for dotfile-managed setups.
func TestCLISetupHeadless_SkipWrapper(t *testing.T) {
	configHome := t.TempDir()
	rcHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "omakiten.db")
	bashrc := filepath.Join(rcHome, ".bashrc")
	writeFile(t, bashrc, "# user content\n")

	t.Setenv("OMAKITEN_HOME", configHome)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", rcHome)
	t.Setenv("OKT_CLI_LANG", "en")
	t.Setenv("OKT_TUI_LANG", "en")
	t.Setenv("OKT_AGENT_LANG", "")
	t.Setenv("OKT_PRESET", "omakase")
	t.Setenv("OKT_HARNESSES", "skip")
	t.Chdir(t.TempDir())

	out := runCLI(t, dbPath, "", "setup", "--skip-wrapper")
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	if _, ok := data["wrapper"]; ok {
		t.Fatalf("expected no wrapper key with --skip-wrapper, got %v", data["wrapper"])
	}

	got := readFile(t, bashrc)
	if strings.Contains(got, "okt wrapper") {
		t.Fatalf("--skip-wrapper still wrote to bashrc: %s", got)
	}
}
