package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"

	"omakiten/internal/domain"
)

// stubBackupRunner counts how many times Run was called so the
// validator-fail tests can pin AC 3: no orphan snapshot when the
// health check aborts the update.
type stubBackupRunner struct {
	calls int
	path  string
}

func (s *stubBackupRunner) Run(context.Context) (string, error) {
	s.calls++
	return s.path, nil
}

// stubValidator returns a fixed result and counts invocations so the
// "skipped when ConfigPath empty" branch can be pinned by call count
// without relying on side effects.
type stubValidator struct {
	result updateValidatorResult
	err    error
	calls  int
}

func (s *stubValidator) fn() updateValidatorFn {
	return func(context.Context, string, string) (updateValidatorResult, error) {
		s.calls++
		return s.result, s.err
	}
}

// TestRunUpdate_ValidatorOKAllowsSwap pins #365 AC 2 happy path: a
// validator that returns OK=true does not block the swap, the backup
// runs after the gate, and the envelope shape stays the existing
// update_completed contract.
func TestRunUpdate_ValidatorOKAllowsSwap(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("posix archive shape")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	archive := tarGzWith(t, map[string][]byte{"okt": []byte("NEW")})
	asset, err := assetName(goruntime.GOOS, goruntime.GOARCH)
	if err != nil {
		t.Fatalf("assetName: %v", err)
	}

	validator := &stubValidator{result: updateValidatorResult{OK: true}}
	backup := &stubBackupRunner{path: "/tmp/backup.db"}
	c := updateClient{
		Fetcher:    stubFetcher{Tag: "0.20.0"},
		Downloader: stubDownloader{Assets: map[string][]byte{asset: archive}},
		Current:    "0.19.0",
		BinaryPath: bin,
		ConfigPath: filepath.Join(dir, "omakase.yaml"),
		Validator:  validator.fn(),
		Backup:     backup,
	}
	res, err := runUpdate(context.Background(), c, updateInputs{Yes: true})
	if err != nil {
		t.Fatalf("runUpdate: %v", err)
	}
	if validator.calls != 1 {
		t.Fatalf("validator calls = %d want 1", validator.calls)
	}
	if backup.calls != 1 {
		t.Fatalf("backup calls = %d want 1 (must run after validator passes)", backup.calls)
	}
	payload, _ := res.(map[string]any)
	if payload["code"] != "update_completed" {
		t.Fatalf("code = %v want update_completed", payload["code"])
	}
	got, _ := os.ReadFile(bin)
	if string(got) != "NEW" {
		t.Fatalf("bin = %q want NEW (swap must complete on validator-ok)", string(got))
	}
}

// TestRunUpdate_ValidatorFailAbortsSwapAndBackup pins #365 AC 2 + AC 3:
// a validator non-zero exit must (a) leave the live binary untouched,
// (b) NOT invoke the backup runner (no orphan snapshot), (c) surface a
// structured envelope with reason=config_validation_failed and the
// errors array intact.
func TestRunUpdate_ValidatorFailAbortsSwapAndBackup(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("posix archive shape")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	archive := tarGzWith(t, map[string][]byte{"okt": []byte("NEW")})
	asset, err := assetName(goruntime.GOOS, goruntime.GOARCH)
	if err != nil {
		t.Fatalf("assetName: %v", err)
	}

	validator := &stubValidator{result: updateValidatorResult{
		OK: false,
		Errors: []map[string]any{
			{
				"kind":              "missing_required_key",
				"path":              "/cfg/omakase.yaml",
				"message":           "config.workflow.active is required",
				"suggested_command": "okt config edit <path>",
			},
		},
		RawOutput: []byte(`{"ok":false}`),
	}}
	backup := &stubBackupRunner{path: "/tmp/backup.db"}
	c := updateClient{
		Fetcher:    stubFetcher{Tag: "0.20.0"},
		Downloader: stubDownloader{Assets: map[string][]byte{asset: archive}},
		Current:    "0.19.0",
		BinaryPath: bin,
		ConfigPath: filepath.Join(dir, "omakase.yaml"),
		Validator:  validator.fn(),
		Backup:     backup,
	}
	_, err = runUpdate(context.Background(), c, updateInputs{Yes: true})
	if err == nil {
		t.Fatalf("expected validator non-zero to abort the update")
	}
	if validator.calls != 1 {
		t.Fatalf("validator calls = %d want 1", validator.calls)
	}
	if backup.calls != 0 {
		t.Fatalf("backup calls = %d want 0 (validator-fail must not leave an orphan snapshot — AC 3)", backup.calls)
	}
	if got, _ := os.ReadFile(bin); string(got) != "OLD" {
		t.Fatalf("bin = %q want OLD (validator-fail must leave the live binary untouched)", string(got))
	}

	var coded *domain.CodedError
	if !errors.As(err, &coded) {
		t.Fatalf("error type = %T want *domain.CodedError", err)
	}
	if coded.Code != domain.ErrUpdateFailed {
		t.Fatalf("code = %v want ErrUpdateFailed", coded.Code)
	}
	if coded.Details["reason"] != "config_validation_failed" {
		t.Fatalf("reason = %v want config_validation_failed", coded.Details["reason"])
	}
	errs, ok := coded.Details["errors"].([]map[string]any)
	if !ok || len(errs) != 1 {
		t.Fatalf("details.errors = %v want single entry", coded.Details["errors"])
	}
	if errs[0]["kind"] != "missing_required_key" {
		t.Fatalf("errors[0].kind = %v want missing_required_key", errs[0]["kind"])
	}
	if errs[0]["suggested_command"] != "okt config edit <path>" {
		t.Fatalf("errors[0].suggested_command = %v want `okt config edit <path>`", errs[0]["suggested_command"])
	}
	if coded.Details["staged_path"] == "" {
		t.Fatalf("details.staged_path missing — caller needs to surface it for cleanup tracing")
	}
	// Staged file must have been cleaned up by the defer guard.
	staged, _ := coded.Details["staged_path"].(string)
	if staged != "" {
		if _, statErr := os.Stat(staged); statErr == nil {
			t.Fatalf("staged binary at %s still present after validator-fail — defer cleanup leaked", staged)
		}
	}
}

// TestRunUpdate_ValidatorExecErrorAbortsSwap pins the AC 2 infra-error
// branch: a Validator that returns a non-nil error (could not spawn,
// unparseable output) surfaces a `config_validation_exec_failed`
// reason and leaves the binary intact. Distinguishes infra failure
// from validator non-zero exit, so the user can tell whether the new
// binary rejected their config or the host could not run the check.
func TestRunUpdate_ValidatorExecErrorAbortsSwap(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("posix archive shape")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	archive := tarGzWith(t, map[string][]byte{"okt": []byte("NEW")})
	asset, err := assetName(goruntime.GOOS, goruntime.GOARCH)
	if err != nil {
		t.Fatalf("assetName: %v", err)
	}

	validator := &stubValidator{err: errors.New("fork/exec: permission denied")}
	backup := &stubBackupRunner{path: "/tmp/backup.db"}
	c := updateClient{
		Fetcher:    stubFetcher{Tag: "0.20.0"},
		Downloader: stubDownloader{Assets: map[string][]byte{asset: archive}},
		Current:    "0.19.0",
		BinaryPath: bin,
		ConfigPath: filepath.Join(dir, "omakase.yaml"),
		Validator:  validator.fn(),
		Backup:     backup,
	}
	_, err = runUpdate(context.Background(), c, updateInputs{Yes: true})
	if err == nil {
		t.Fatalf("expected validator exec error to abort the update")
	}
	if backup.calls != 0 {
		t.Fatalf("backup calls = %d want 0 (no snapshot when health check could not even run)", backup.calls)
	}
	if got, _ := os.ReadFile(bin); string(got) != "OLD" {
		t.Fatalf("bin = %q want OLD", string(got))
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrUpdateFailed {
		t.Fatalf("error: %v want ErrUpdateFailed", err)
	}
	if coded.Details["reason"] != "config_validation_exec_failed" {
		t.Fatalf("reason = %v want config_validation_exec_failed", coded.Details["reason"])
	}
}

// TestRunUpdate_EmptyConfigPathSkipsValidator pins the back-compat
// branch: a client without a ConfigPath (e.g. tests that exercise the
// swap directly without an associated config) must not invoke the
// validator and must complete the swap as before. Production wiring
// always sets ConfigPath, so this only guards the existing
// `TestRunUpdate_YesSwapsBinary`-style tests against an accidental
// Validator panic from a stub that never fires.
func TestRunUpdate_EmptyConfigPathSkipsValidator(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("posix archive shape")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	archive := tarGzWith(t, map[string][]byte{"okt": []byte("NEW")})
	asset, err := assetName(goruntime.GOOS, goruntime.GOARCH)
	if err != nil {
		t.Fatalf("assetName: %v", err)
	}

	validator := &stubValidator{err: errors.New("should never be called")}
	c := updateClient{
		Fetcher:    stubFetcher{Tag: "0.20.0"},
		Downloader: stubDownloader{Assets: map[string][]byte{asset: archive}},
		Current:    "0.19.0",
		BinaryPath: bin,
		ConfigPath: "",
		Validator:  validator.fn(),
	}
	if _, err := runUpdate(context.Background(), c, updateInputs{Yes: true}); err != nil {
		t.Fatalf("runUpdate: %v", err)
	}
	if validator.calls != 0 {
		t.Fatalf("validator calls = %d want 0 (empty ConfigPath must skip the gate)", validator.calls)
	}
}
