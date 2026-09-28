package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
)

func TestDefaultUpdateValidatorRejectsMissingMalformedOrEmptyOutput(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("POSIX candidate executable")
	}
	for name, body := range map[string]string{
		"missing executable": "",
		"empty output":       "#!/bin/sh\nexit 0\n",
		"malformed output":   "#!/bin/sh\nprintf 'not JSON'\n",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			script := filepath.Join(root, "validator")
			if body != "" {
				if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			result, err := defaultUpdateValidator(context.Background(), script, filepath.Join(root, "config.yaml"))
			if err == nil || result.OK {
				t.Fatalf("unusable validator approved swap: %+v, %v", result, err)
			}
		})
	}
}

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

func TestDefaultUpdateValidatorUsesCurrentOnlyInvocation(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("posix-only shell script harness")
	}
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	script := filepath.Join(dir, "validator.sh")
	body := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + argsPath + "\"\n" +
		"echo '{\"ok\":true}'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	configPath := filepath.Join(dir, "cfg.yaml")
	if _, err := defaultUpdateValidator(context.Background(), script, configPath); err != nil {
		t.Fatalf("defaultUpdateValidator: %v", err)
	}
	got, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	want := "config\nvalidate\n--config\n" + configPath + "\n"
	if string(got) != want {
		t.Fatalf("validator args = %q, want %q", string(got), want)
	}
}

func TestHistoricalV030ValidatorSubprocessUsesExactMigrationInvocation(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("posix-only shell script harness")
	}
	for _, outcome := range []struct {
		name string
		ok   bool
	}{
		{name: "success", ok: true},
		{name: "failure", ok: false},
	} {
		outcome := outcome
		t.Run(outcome.name, func(t *testing.T) { runHistoricalV030ValidatorSubprocess(t, outcome.ok) })
	}
}

func runHistoricalV030ValidatorSubprocess(t *testing.T, wantOK bool) {
	t.Helper()
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "staged-args")
	staged := filepath.Join(dir, "staged.sh")
	writeExecutable(t, staged, "#!/bin/sh\nprintf '%s\\n' \"$@\" > \""+argsPath+"\"\n"+
		"if [ \"$HISTORICAL_RESULT\" = failure ]; then echo '{\"ok\":false,\"details\":{\"errors\":[{\"kind\":\"invalid_value\"}]}}'; exit 1; fi\n"+
		"echo '{\"ok\":true}'\n")
	oldUpdater := filepath.Join(dir, "old-updater.sh")
	writeExecutable(t, oldUpdater, "#!/bin/sh\nstaged=\"$1\"\nconfig=\"$2\"\nargs=\"$3\"\n\"$staged\" config validate --migrate --config \"$config\" > \"$args.output\"\nstatus=$?\ncat \"$args.output\"\nexit $status\n")
	configPath := filepath.Join(dir, "config", "omakase.yaml")
	args := exec.CommandContext(context.Background(), oldUpdater, staged, configPath, argsPath, "--skip-defaults")
	args.Env = append(os.Environ(), "HISTORICAL_RESULT="+map[bool]string{true: "success", false: "failure"}[wantOK])
	output, err := args.CombinedOutput()
	if wantOK != (err == nil) {
		t.Fatalf("historical updater status: err=%v, output=%s", err, output)
	}
	result, err := parseValidatorOutput(output, !wantOK)
	if err != nil {
		t.Fatalf("parse historical validator output: %v, output=%s", err, output)
	}
	if result.OK != wantOK {
		t.Fatalf("historical validator OK=%t, want %t", result.OK, wantOK)
	}
	got, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read staged args: %v", err)
	}
	want := "config\nvalidate\n--migrate\n--config\n" + configPath + "\n"
	if string(got) != want {
		t.Fatalf("historical staged args=%q, want=%q", got, want)
	}
	if strings.Contains(string(got), "skip-defaults") || !bytes.Contains(output, []byte(`"ok"`)) {
		t.Fatalf("historical invocation forwarded skip-defaults or omitted envelope: args=%q output=%q", got, output)
	}
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
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

func TestDefaultUpdateValidator_NonZeroExitCannotReportSuccess(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("posix-only shell script harness")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "validator.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho '{\"ok\":true}'\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	result, err := defaultUpdateValidator(context.Background(), script, filepath.Join(dir, "cfg.yaml"))
	if err != nil {
		t.Fatalf("unexpected error path: %v", err)
	}
	if result.OK {
		t.Fatal("OK = true, want false when the validator exits non-zero")
	}
}

func TestDefaultUpdateValidator_CanceledContextIsInfrastructureFailure(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("posix-only shell script harness")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "validator.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 10\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := defaultUpdateValidator(ctx, script, filepath.Join(dir, "cfg.yaml"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled infrastructure failure", err)
	}
	if result.OK {
		t.Fatal("OK = true, want false on canceled context")
	}
}

func TestDefaultUpdateValidator_SignalTerminationIsInfrastructureFailure(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("signal semantics differ on Windows")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "validator.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nkill -TERM $$\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	result, err := defaultUpdateValidator(context.Background(), script, filepath.Join(dir, "cfg.yaml"))
	if err == nil {
		t.Fatal("signal termination returned nil error")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ProcessState == nil || exitErr.ExitCode() >= 0 {
		t.Fatalf("error = %v, want signaled exec.ExitError", err)
	}
	if result.OK {
		t.Fatal("OK = true, want false on signal termination")
	}
}
