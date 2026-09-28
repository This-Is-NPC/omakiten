package cli

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIConfigDiagnosticsNameTheFileAndRepair(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config with spaces", "config", "custom", "example.yaml")
	cases := map[string]string{
		"unknown_schema_key":   "field unsupported not found in type config.Settings",
		"missing_required_key": "config.workflow.active is required",
		"theme_not_found":      "active theme does not exist",
		"invalid_value":        "value must be between 1 and 10",
		"validation":           "unmarshal errors: invalid document",
	}
	for kind, message := range cases {
		t.Run(kind, func(t *testing.T) {
			details := buildValidateFailureDetails(path, errors.New(message), nil)
			entry := details["errors"].([]map[string]any)[0]
			if entry["kind"] != kind {
				t.Fatalf("kind: %v, want %s", entry["kind"], kind)
			}
			command := entry["suggested_command"].(string)
			hint := entry["hint"].(string)
			if command != "${EDITOR:-vi} "+shellQuoteArg(path) || !strings.Contains(hint, command) || strings.Contains(hint, "%!") || strings.Contains(hint, "cli.config.validate.remediation") {
				t.Fatalf("unusable repair: %v", entry)
			}
		})
	}
	missing := filepath.Join(root, "config", "omakase.yaml")
	details := buildValidateFailureDetails(missing, errors.New("open missing asset: no such file"), nil)
	entry := details["errors"].([]map[string]any)[0]
	if entry["suggested_command"] != updateDefaultsManualCommandForConfig(missing) {
		t.Fatalf("refresh scope: %v", entry)
	}
}
