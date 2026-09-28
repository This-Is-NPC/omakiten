package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLISetupFlagsOverrideEnvironmentAndReportInvalidChoices(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("OMAKITEN_HOME", filepath.Join(root, "installation"))
	t.Setenv("HOME", root)
	t.Setenv("OKT_CLI_LANG", "unavailable")
	t.Setenv("OKT_TUI_LANG", "unavailable")
	t.Setenv("OKT_AGENT_LANG", "environment directive")
	t.Setenv("OKT_PRESET", "unknown-preset")
	t.Setenv("OKT_HARNESSES", "unknown-harness")
	db := filepath.Join(root, "state.db")
	base := []string{"setup", "--cli-lang", "en", "--tui-lang", "en", "--agent-lang", "", "--skip-wrapper"}
	out := runCLI(t, db, "", base...)
	data := decodeEnvelope(t, out)["data"].(map[string]any)
	if data["preset"].(map[string]any)["name"] != "omakase" || data["languages"].(map[string]any)["agent_output"] != "" || data["harnesses_planned"] != nil {
		t.Fatalf("flag and fallback selections: %v", data)
	}
	for _, flag := range []string{"cli-lang", "tui-lang"} {
		t.Run(flag, func(t *testing.T) {
			args := append(append([]string{}, base...), "--"+flag, "unknown-language", "--update")
			envelope := runCLIExpectError(t, db, "", "validation_error", args...)
			if !strings.Contains(envelope["msg"].(string), flag) {
				t.Fatalf("invalid surface absent: %v", envelope)
			}
		})
	}
}

func TestCLIConfigInitIsIdempotentAndRefreshesOnForce(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	db, cfg := filepath.Join(root, "state.db"), filepath.Join(root, "config", "omakase.yaml")
	args := []string{"config", "init", "--scope", "global", "--preset", "omakase", "--cli-lang", "", "--tui-lang", "", "--agent-lang", ""}
	runCLI(t, db, cfg, args...)
	before := readFile(t, cfg)
	out := decodeEnvelope(t, runCLI(t, db, cfg, args...))["data"].(map[string]any)
	if out["no_op"] != true {
		t.Fatalf("repeated init changed existing preset: %v", out)
	}
	readBackEquals(t, cfg, before)
	out = decodeEnvelope(t, runCLI(t, db, cfg, append(args, "--force")...))["data"].(map[string]any)
	if out["refreshed"] != true {
		t.Fatalf("forced refresh absent: %v", out)
	}
	runCLIExpectError(t, db, cfg, "validation_error", "config", "init", "--scope", "global", "--preset", "missing")
	runCLIExpectError(t, db, cfg, "validation_error", "config", "init", "--scope", "global", "--preset", "omakase", "--tui-lang", "missing")
	readBackEquals(t, cfg, before)
}

func TestCLISetupPickerSkipsBrokenCustomLanguageFiles(t *testing.T) {
	root := t.TempDir()
	t.Setenv("OMAKITEN_HOME", root)
	dir := filepath.Join(root, "languages", "custom")
	writeFile(t, filepath.Join(dir, "broken.yaml"), "code: [\n")
	writeFile(t, filepath.Join(dir, "uppercase.yml"), "code: Uppercase\n")
	writeFile(t, filepath.Join(dir, "missing-code.yaml"), "name: No code\n")
	writeFile(t, filepath.Join(dir, "notes.txt"), "user notes\n")
	writeFile(t, filepath.Join(dir, "short-name.yaml"), "code: xy\nname: Custom language\n")
	writeFile(t, filepath.Join(dir, "code-only.yaml"), "code: zz\n")
	if err := os.Mkdir(filepath.Join(dir, "directory.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	options, err := loadBundledLanguageOptions()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, option := range options {
		names[option.Code] = option.Native
	}
	if names["en"] == "" || names["xy"] != "Custom language" || names["zz"] != "zz" || names["Uppercase"] != "" || names[""] != "" {
		t.Fatalf("invalid custom language affected choices: %v", names)
	}
}
