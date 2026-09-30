package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"omakiten/internal/operation"
)

func TestWriteErrorOperationDenied(t *testing.T) {
	cmd := &cobra.Command{Use: "okt"}
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := writeError(cmd, operation.OperationDenied{
		Surface: operation.SurfaceCLI,
		Op:      "template.list",
		Reason:  "${{intl:operations.denied.agent_delete}}",
	})
	code, ok := ExitCode(err)
	if !ok || code != 1 {
		t.Fatalf("ExitCode = (%d, %v), want (1, true)", code, ok)
	}

	envelope := decodeEnvelope(t, out.String())
	if envelope["ok"] != false {
		t.Fatalf("ok = %v, want false", envelope["ok"])
	}
	if envelope["code"] != "operation_denied" {
		t.Fatalf("code = %v, want operation_denied; envelope=%v", envelope["code"], envelope)
	}
	msg, _ := envelope["msg"].(string)
	if msg == "" {
		t.Fatalf("msg empty; envelope=%v", envelope)
	}
	if !strings.Contains(msg, "Agents cannot delete this resource.") && !strings.Contains(msg, "operations.denied.agent_delete") {
		t.Fatalf("msg = %q, want resolved reason or intl token", msg)
	}
	details, _ := envelope["details"].(map[string]any)
	if details["op"] != "template.list" {
		t.Fatalf("details.op = %v, want template.list", details["op"])
	}
	if details["surface"] != "cli" {
		t.Fatalf("details.surface = %v, want cli", details["surface"])
	}
	reason, _ := details["reason"].(string)
	if reason == "" {
		t.Fatalf("details.reason empty; envelope=%v", envelope)
	}
}

func TestWriteErrorOperationDeniedWrapped(t *testing.T) {
	cmd := &cobra.Command{Use: "okt"}
	var out bytes.Buffer
	cmd.SetOut(&out)

	inner := operation.OperationDenied{
		Surface: operation.SurfaceCLI,
		Op:      "task.delete",
		Reason:  "cli off",
	}
	err := writeError(cmd, fmt.Errorf("delete task: %w", inner))
	code, ok := ExitCode(err)
	if !ok || code != 1 {
		t.Fatalf("ExitCode = (%d, %v), want (1, true)", code, ok)
	}

	envelope := decodeEnvelope(t, out.String())
	if envelope["code"] != "operation_denied" {
		t.Fatalf("code = %v, want operation_denied; envelope=%v", envelope["code"], envelope)
	}
	if envelope["msg"] != "cli off" {
		t.Fatalf("msg = %v, want %q", envelope["msg"], "cli off")
	}
	details, _ := envelope["details"].(map[string]any)
	if details["op"] != "task.delete" || details["surface"] != "cli" {
		t.Fatalf("details = %v", details)
	}
}

func TestWriteErrorOperationDeniedEmptyReason(t *testing.T) {
	cmd := &cobra.Command{Use: "okt"}
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := writeError(cmd, operation.OperationDenied{
		Surface: operation.SurfaceCLI,
		Op:      "template.list",
	})
	if err == nil {
		t.Fatal("writeError returned nil")
	}

	envelope := decodeEnvelope(t, out.String())
	if envelope["code"] != "operation_denied" {
		t.Fatalf("code = %v, want operation_denied; envelope=%v", envelope["code"], envelope)
	}
	msg, _ := envelope["msg"].(string)
	if msg == "" {
		t.Fatalf("msg empty; envelope=%v", envelope)
	}
	details, _ := envelope["details"].(map[string]any)
	if details["op"] != "template.list" || details["surface"] != "cli" {
		t.Fatalf("details = %v", details)
	}
	if _, ok := details["reason"]; ok {
		t.Fatalf("details.reason = %v, want omitted when Reason is empty", details["reason"])
	}
}

func TestCLIDeniedOperationNamedError(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectRoot) error = %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")

	mod := filepath.Join(tmp, "config", "modules", "surfaces.yaml")
	raw, err := os.ReadFile(mod)
	if err != nil {
		t.Fatalf("read surfaces module: %v", err)
	}
	patched := strings.Replace(string(raw),
		"template.list: { cli: true, tui: true, http: true }",
		`template.list: { cli: false, tui: true, http: true, reason: "${{intl:operations.denied.agent_delete}}" }`,
		1)
	if patched == string(raw) {
		t.Fatal("surfaces module did not contain the template.list row to patch")
	}
	if err := os.WriteFile(mod, []byte(patched), 0o644); err != nil {
		t.Fatalf("write patched surfaces: %v", err)
	}

	help := NewRootCommand("test", Runners{})
	var helpOut bytes.Buffer
	help.SetOut(&helpOut)
	help.SetArgs([]string{"template", "list", "--help"})
	if err := help.Execute(); err != nil {
		t.Fatalf("template list --help: %v out=%s", err, helpOut.String())
	}

	envelope := runCLIExpectError(t, dbPath, configPath, "operation_denied", "template", "list")
	msg, _ := envelope["msg"].(string)
	if msg == "" {
		t.Fatalf("msg empty; envelope=%v", envelope)
	}
	if !strings.Contains(msg, "Agents cannot delete this resource.") && !strings.Contains(msg, "operations.denied.agent_delete") {
		t.Fatalf("msg = %q, want resolved reason or intl token", msg)
	}
	details, _ := envelope["details"].(map[string]any)
	if details["op"] != "template.list" {
		t.Fatalf("details.op = %v, want template.list; envelope=%v", details["op"], envelope)
	}
	if details["surface"] != "cli" {
		t.Fatalf("details.surface = %v, want cli; envelope=%v", details["surface"], envelope)
	}
	reason, _ := details["reason"].(string)
	if reason == "" {
		t.Fatalf("details.reason empty; envelope=%v", envelope)
	}
}
