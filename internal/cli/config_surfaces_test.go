package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"omakiten/internal/config"
)

func TestConfigSurfacesScaffoldEmitsCanonicalTable(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"config", "surfaces", "--scaffold"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v out=%s", err, out.String())
	}

	raw := out.String()
	dec := yaml.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.KnownFields(true)
	var table config.SurfaceTable
	if err := dec.Decode(&table); err != nil {
		t.Fatalf("decode scaffold yaml: %v\n%s", err, raw)
	}
	if len(table) != config.CanonicalSurfaceCount {
		t.Fatalf("scaffold slugs = %d, want %d", len(table), config.CanonicalSurfaceCount)
	}
	assertCanonicalSurfaceTable(t, table)
}

func assertCanonicalSurfaceTable(t *testing.T, table config.SurfaceTable) {
	for slug, wantRow := range config.CanonicalSurfaceTable() {
		got, ok := table[slug]
		if !ok {
			t.Errorf("scaffold missing %q", slug)
			continue
		}
		assertCanonicalSurfaceRow(t, slug, got, wantRow)
	}
}

func assertCanonicalSurfaceRow(t *testing.T, slug string, got, want config.SurfacePolicy) {
	if want.Reason != "" {
		if got.Reason != want.Reason {
			t.Errorf("%s: reason = %q, want %q", slug, got.Reason, want.Reason)
		}
		if got.CLI == nil || *got.CLI || got.TUI == nil || *got.TUI {
			t.Errorf("%s: wiring row = %+v, want all false", slug, got)
		}
		return
	}
	if got.CLI == nil || !*got.CLI || got.TUI == nil || !*got.TUI {
		t.Errorf("%s: product row = %+v, want all true", slug, got)
	}
	if strings.TrimSpace(got.Reason) != "" {
		t.Errorf("%s: product reason = %q, want empty", slug, got.Reason)
	}
}

func TestConfigSurfacesCheckMatchingTableIsOK(t *testing.T) {
	tmp := t.TempDir()
	if err := config.EnsureDefaultFiles(tmp); err != nil {
		t.Fatalf("EnsureDefaultFiles: %v", err)
	}
	cfgPath := filepath.Join(tmp, "config", "omakase.yaml")
	dbPath := filepath.Join(tmp, "omakiten.db")

	out := runCLI(t, dbPath, cfgPath, "config", "surfaces", "--check")
	envelope := decodeEnvelope(t, out)
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %v", envelope["data"])
	}
	if data["ok"] != true {
		t.Fatalf("data.ok = %v, want true; out=%s", data["ok"], out)
	}
	if data["path"] != cfgPath {
		t.Fatalf("data.path = %v, want %s", data["path"], cfgPath)
	}
	for _, key := range []string{"missing", "extra"} {
		list, ok := data[key].([]any)
		if !ok {
			t.Fatalf("data.%s missing or wrong type: %v", key, data[key])
		}
		if len(list) != 0 {
			t.Fatalf("data.%s = %v, want empty", key, list)
		}
	}
}

func TestConfigSurfacesCheckMissingSlugIsError(t *testing.T) {
	tmp := t.TempDir()
	if err := config.EnsureDefaultFiles(tmp); err != nil {
		t.Fatalf("EnsureDefaultFiles: %v", err)
	}
	mod := filepath.Join(tmp, "config", "modules", "surfaces.yaml")
	raw, err := os.ReadFile(mod)
	if err != nil {
		t.Fatalf("read surfaces module: %v", err)
	}
	stripped := strings.Replace(string(raw), "task.delete: { cli: true, tui: true, }\n", "", 1)
	if stripped == string(raw) {
		t.Fatal("surfaces module did not contain the task.delete row to strip")
	}
	if err := os.WriteFile(mod, []byte(stripped), 0o644); err != nil {
		t.Fatalf("write stripped surfaces module: %v", err)
	}

	cfgPath := filepath.Join(tmp, "config", "omakase.yaml")
	dbPath := filepath.Join(tmp, "omakiten.db")
	envelope := runCLIExpectError(t, dbPath, cfgPath, "config_invalid", "config", "surfaces", "--check")
	msg, _ := envelope["msg"].(string)
	if !strings.Contains(msg, "task.delete") {
		t.Fatalf("error msg = %q, want it to mention task.delete", msg)
	}
	details, _ := envelope["details"].(map[string]any)
	missing, _ := details["missing"].([]any)
	found := false
	for _, item := range missing {
		if item == "task.delete" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("details.missing = %v, want to include task.delete; envelope=%v", missing, envelope)
	}
}

func TestConfigSurfacesRequiresExactlyOneFlag(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"config", "surfaces"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("execute: expected error when neither --scaffold nor --check is set, out=%s", out.String())
	}
}

func TestConfigSurfacesRejectsBothFlags(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"config", "surfaces", "--scaffold", "--check"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("execute: expected error when both flags are set, out=%s", out.String())
	}
}
