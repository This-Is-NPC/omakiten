package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIConfigInitFlagsSetLanguages(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	t.Chdir(t.TempDir())

	out := runCLI(t, dbPath, globalConfig,
		"config", "init",
		"--scope", "global",
		"--preset", "omakase",
		"--cli-lang", "en",
		"--tui-lang", "en",
		"--agent-lang", "Português (Brasil)",
	)
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	languages, ok := data["languages"].(map[string]any)
	if !ok {
		t.Fatalf("expected languages payload, got %v", data)
	}
	if languages["agent_output"] != "Português (Brasil)" {
		t.Fatalf("agent_output: got %v, want Português (Brasil)", languages["agent_output"])
	}

	// Round-trip: subsequent show should reflect the persisted block.
	out = runCLI(t, dbPath, globalConfig, "config", "language", "show")
	envelope = decodeEnvelope(t, out)
	data = envelope["data"].(map[string]any)
	languages = data["languages"].(map[string]any)
	if languages["agent_output"] != "Português (Brasil)" {
		t.Fatalf("show after init: got %v, want Português (Brasil)", languages["agent_output"])
	}
}

func TestCLIConfigInitRejectsUnknownCLILang(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	t.Chdir(t.TempDir())

	envelope := runCLIExpectError(t, dbPath, globalConfig, "validation_error",
		"config", "init",
		"--scope", "global",
		"--preset", "omakase",
		"--cli-lang", "xx",
	)
	if msg, _ := envelope["msg"].(string); !strings.Contains(msg, "xx") || !strings.Contains(msg, "cli-lang") {
		t.Fatalf("expected cli-lang rejection for xx, got %v", envelope)
	}
}

func TestCLIConfigInitWithoutLangFlagsOmitsBlock(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	t.Chdir(t.TempDir())

	out := runCLI(t, dbPath, globalConfig,
		"config", "init",
		"--scope", "global",
		"--preset", "omakase",
	)
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	// When no flags are supplied and stdin is non-interactive (the test
	// harness pipes stdin from os.DevNull), init should not emit a
	// languages payload since nothing changed from the seeded defaults.
	if _, ok := data["languages"]; ok {
		t.Fatalf("expected no languages payload when flags omitted, got %v", data["languages"])
	}
}
