package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIConfigLanguageShowReturnsDefaults(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	t.Chdir(t.TempDir())

	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "global", "--preset", "omakase")

	out := runCLI(t, dbPath, globalConfig, "config", "language", "show")
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)

	languages := data["languages"].(map[string]any)
	if languages["cli"] != "en" {
		t.Fatalf("cli default: got %v, want en", languages["cli"])
	}
	if languages["tui"] != "en" {
		t.Fatalf("tui default: got %v, want en", languages["tui"])
	}
	if languages["agent_output"] != "" {
		t.Fatalf("agent_output default: got %v, want empty", languages["agent_output"])
	}
	available := data["available"].([]any)
	if len(available) == 0 {
		t.Fatalf("available languages empty after init; expected at least en")
	}
	foundEn := false
	for _, entry := range available {
		row := entry.(map[string]any)
		if row["code"] == "en" {
			foundEn = true
			break
		}
	}
	if !foundEn {
		t.Fatalf("available codes lack en; got %v", available)
	}
}

func TestCLIConfigLanguageSetAgentFreeForm(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	t.Chdir(t.TempDir())

	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "global", "--preset", "omakase")

	out := runCLI(t, dbPath, globalConfig, "config", "language", "set", "--agent", "Português (Brasil)", "--global")
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	languages := data["languages"].(map[string]any)
	if languages["agent_output"] != "Português (Brasil)" {
		t.Fatalf("set agent_output: got %v, want Português (Brasil)", languages["agent_output"])
	}

	// show should reflect the persisted value.
	out = runCLI(t, dbPath, globalConfig, "config", "language", "show")
	envelope = decodeEnvelope(t, out)
	data = envelope["data"].(map[string]any)
	languages = data["languages"].(map[string]any)
	if languages["agent_output"] != "Português (Brasil)" {
		t.Fatalf("show after set: got %v, want Português (Brasil)", languages["agent_output"])
	}
}

func TestCLIConfigLanguageSetRequiresAtLeastOneFlag(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	t.Chdir(t.TempDir())

	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "global", "--preset", "omakase")
	envelope := runCLIExpectError(t, dbPath, globalConfig, "validation_error", "config", "language", "set")
	if msg, _ := envelope["msg"].(string); !strings.Contains(msg, "at least one") {
		t.Fatalf("expected at-least-one-flag error, got %v", envelope)
	}
}

func TestCLIConfigLanguageSetRejectsUnknownCLICode(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	t.Chdir(t.TempDir())

	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "global", "--preset", "omakase")
	envelope := runCLIExpectError(t, dbPath, globalConfig, "validation_error", "config", "language", "set", "--cli", "xx", "--global")
	if msg, _ := envelope["msg"].(string); !strings.Contains(msg, "xx") || !strings.Contains(msg, "cli") {
		t.Fatalf("expected unknown-code error for --cli xx, got %v", envelope)
	}
}

func TestCLIConfigLanguageResetRemovesCustomValues(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	t.Chdir(t.TempDir())

	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "global", "--preset", "omakase")
	runCLI(t, dbPath, globalConfig, "config", "language", "set", "--agent", "English", "--global")

	out := runCLI(t, dbPath, globalConfig, "config", "language", "reset", "--global")
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	languages := data["languages"].(map[string]any)
	if languages["agent_output"] != "" {
		t.Fatalf("reset should clear agent_output, got %v", languages["agent_output"])
	}
	if languages["cli"] != "en" || languages["tui"] != "en" {
		t.Fatalf("reset should restore en defaults, got %v", languages)
	}
}
