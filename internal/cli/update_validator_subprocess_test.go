package cli

import (
	"context"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"
)

// TestDefaultUpdateValidator_IgnoresStderrNoise pins review-finding
// (error) on stdout/stderr split. A staged binary that writes
// `emitBundleWarnings` output to stderr while still emitting the
// envelope on stdout must parse cleanly — pre-fix the shared
// `cmd.Stderr = cmd.Stdout` buffer prepended the warning text to the
// JSON and broke `json.Unmarshal` with `invalid character 'W'`.
func TestDefaultUpdateValidator_IgnoresStderrNoise(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("posix-only shell script harness")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "validator.sh")
	body := "#!/bin/sh\n" +
		"echo \"WARN: notifications.kit_notifications.foo.message: stub warning\" 1>&2\n" +
		"echo \"WARN: another shipped-file drift line\" 1>&2\n" +
		"echo '{\"ok\":true,\"data\":{\"path\":\"/tmp/cfg.yaml\",\"errors\":[],\"warnings\":[]}}'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	result, err := defaultUpdateValidator(context.Background(), script, filepath.Join(dir, "cfg.yaml"))
	if err != nil {
		t.Fatalf("defaultUpdateValidator: %v (raw=%q)", err, string(result.RawOutput))
	}
	if !result.OK {
		t.Fatalf("OK = false, want true (stderr noise must not poison the JSON parse). RawOutput=%q", string(result.RawOutput))
	}
}

// TestDefaultUpdateValidator_NonZeroExitWithEmptyStdoutReturnsStructuredFail
// pins review-finding (info) on the empty-output non-zero exit path.
// Pre-fix the wrapper returned `validator produced no output` as a Go
// error, mis-routing the failure to `config_validation_exec_failed`
// instead of the structured `config_validation_failed` envelope.
func TestDefaultUpdateValidator_NonZeroExitWithEmptyStdoutReturnsStructuredFail(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("posix-only shell script harness")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "validator.sh")
	body := "#!/bin/sh\nexit 1\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	result, err := defaultUpdateValidator(context.Background(), script, filepath.Join(dir, "cfg.yaml"))
	if err != nil {
		t.Fatalf("unexpected error path: %v — non-zero exit + empty stdout must surface as OK=false, not an infra error", err)
	}
	if result.OK {
		t.Fatalf("OK = true, want false on non-zero exit")
	}
}
