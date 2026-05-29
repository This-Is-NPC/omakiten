package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConfigValidateMigrateMaterialisesFreshRoot pins #365 AC 1.
//
// `okt config validate --migrate <path>` against an empty config root
// must run MigrateLayout + EnsureDefaultFiles, materialise the shipped
// preset yaml at <path>, then LoadBundle + ValidateBundle without
// surfacing an error. The success envelope ships `{data: {path,
// errors: [], warnings: [...]}}` so the caller (`okt update`) can
// gate the pre-swap step on `ok:true` + empty errors.
func TestConfigValidateMigrateMaterialisesFreshRoot(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config", "omakase.yaml")

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--config", cfgPath, "config", "validate", "--migrate"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v out=%s", err, out.String())
	}

	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("expected --migrate to materialise %s, stat err = %v", cfgPath, err)
	}

	var env map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v out=%s", err, out.String())
	}
	if env["ok"] != true {
		t.Fatalf("ok=%v want true, out=%s", env["ok"], out.String())
	}
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %v", env["data"])
	}
	if data["path"] != cfgPath {
		t.Fatalf("data.path=%v want %s", data["path"], cfgPath)
	}
	errs, ok := data["errors"].([]any)
	if !ok {
		t.Fatalf("data.errors missing or wrong type: %v", data["errors"])
	}
	if len(errs) != 0 {
		t.Fatalf("data.errors len=%d want 0 (success path), entries=%v", len(errs), errs)
	}
	if _, ok := data["warnings"]; !ok {
		t.Fatalf("data.warnings missing (must always be present, even when empty)")
	}
}

// TestConfigValidateMigrateBrokenYamlReturnsStructuredEnvelope pins
// #365 AC 1 + AC 9: a validator failure under --migrate surfaces an
// `ok:false` envelope whose `details.errors` slice carries the
// {kind, path, message, suggested_command} shape the remediation
// catalogue mandates.
//
// Uses `version: 2` because ValidateBundle hard-rejects the wrong
// version with "version must be 1" — a stable, well-scoped failure
// path that exercises the same code shape a real schema drift would.
func TestConfigValidateMigrateBrokenYamlReturnsStructuredEnvelope(t *testing.T) {
	tmp := t.TempDir()
	cfgDir := filepath.Join(tmp, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir cfgDir: %v", err)
	}
	cfgPath := filepath.Join(cfgDir, "omakase.yaml")
	if err := os.WriteFile(cfgPath, []byte("version: 2\n"), 0o644); err != nil {
		t.Fatalf("write broken yaml: %v", err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--config", cfgPath, "config", "validate", "--migrate"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("execute: expected non-nil exit error on validator fail, out=%s", out.String())
	}

	var env map[string]any
	if jsonErr := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &env); jsonErr != nil {
		t.Fatalf("unmarshal envelope: %v out=%s", jsonErr, out.String())
	}
	if env["ok"] != false {
		t.Fatalf("ok=%v want false, out=%s", env["ok"], out.String())
	}
	if env["code"] != "config_invalid" {
		t.Fatalf("code=%v want config_invalid, out=%s", env["code"], out.String())
	}
	details, ok := env["details"].(map[string]any)
	if !ok {
		t.Fatalf("details missing or wrong type: %v", env["details"])
	}
	errs, ok := details["errors"].([]any)
	if !ok {
		t.Fatalf("details.errors missing or wrong type: %v", details["errors"])
	}
	if len(errs) != 1 {
		t.Fatalf("details.errors len=%d want 1, entries=%v", len(errs), errs)
	}
	entry, ok := errs[0].(map[string]any)
	if !ok {
		t.Fatalf("details.errors[0] wrong type: %v", errs[0])
	}
	kind, _ := entry["kind"].(string)
	if kind == "" {
		t.Fatalf("details.errors[0].kind empty, entry=%v", entry)
	}
	if _, found := healthCheckRemediations[kind]; !found {
		t.Fatalf("kind %q not in healthCheckRemediations catalogue (contract bug)", kind)
	}
	cmdStr, _ := entry["suggested_command"].(string)
	if strings.TrimSpace(cmdStr) == "" {
		t.Fatalf("details.errors[0].suggested_command empty for kind %q", kind)
	}
	msg, _ := entry["message"].(string)
	if !strings.Contains(strings.ToLower(msg), "version") {
		t.Fatalf("details.errors[0].message = %q, want it to mention 'version'", msg)
	}
	if entry["path"] != cfgPath {
		t.Fatalf("details.errors[0].path = %v want %s", entry["path"], cfgPath)
	}
	hint, _ := entry["hint"].(string)
	if strings.TrimSpace(hint) == "" {
		t.Fatalf("details.errors[0].hint empty for kind %q — #367 i18n hint missing", kind)
	}
	if !strings.Contains(hint, cmdStr) {
		t.Fatalf("details.errors[0].hint = %q must embed suggested_command %q", hint, cmdStr)
	}
}

// TestClassifyValidationErrorMapsKnownPatterns locks the substring
// classifier against drift. Each branch maps to a kind that has a
// healthCheckRemediations entry — silent demotion to "validation"
// would surface a generic hint instead of the targeted command.
func TestClassifyValidationErrorMapsKnownPatterns(t *testing.T) {
	cases := []struct {
		msg  string
		want string
	}{
		{"config.theme.active is required", "theme_not_found"},
		{"unknown field 'foo' in config.context", "unknown_schema_key"},
		{"config.workflow.active is required", "missing_required_key"},
		{"config.context.default_level must be between 1 and 3", "invalid_value"},
		{"open /tmp/missing.yaml: no such file or directory", "missing_shipped_file"},
		{"some unrelated failure", "validation"},
	}
	for _, tc := range cases {
		got := classifyValidationError(stringErr(tc.msg))
		if got != tc.want {
			t.Errorf("classify(%q) = %q, want %q", tc.msg, got, tc.want)
		}
		if _, found := healthCheckRemediations[got]; !found {
			t.Errorf("classify(%q) returned %q with no healthCheckRemediations entry", tc.msg, got)
		}
	}
}

type stringErr string

func (s stringErr) Error() string { return string(s) }
