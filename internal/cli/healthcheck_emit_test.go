package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"omakiten/internal/domain"
)

// stubEventStore records every RecordEntityEvent call so emission
// tests can pin shape + count. Err is the sticky return value used to
// exercise the activity-write-failure graceful-degrade path (#369 AC 6).
type stubEventStore struct {
	calls []recordedEvent
	err   error
}

type recordedEvent struct {
	entityType string
	eventType  string
	payload    map[string]any
}

func (s *stubEventStore) RecordEntityEvent(_ context.Context, entityType string, _, _ int64, eventType, payload string) error {
	rec := recordedEvent{entityType: entityType, eventType: eventType}
	_ = json.Unmarshal([]byte(payload), &rec.payload)
	s.calls = append(s.calls, rec)
	return s.err
}

func (s *stubEventStore) countByType(eventType string) int {
	n := 0
	for _, c := range s.calls {
		if c.eventType == eventType {
			n++
		}
	}
	return n
}

func (s *stubEventStore) firstByType(eventType string) (recordedEvent, bool) {
	for _, c := range s.calls {
		if c.eventType == eventType {
			return c, true
		}
	}
	return recordedEvent{}, false
}

// TestRunUpdate_EmitsHealthCheckPassedAndSwapCompletedOnSuccess pins
// #369 AC 5: a clean upgrade emits update.healthcheck.passed exactly
// once + update.swap.completed exactly once, and does NOT emit
// healthcheck.failed / swap.aborted on the success path.
func TestRunUpdate_EmitsHealthCheckPassedAndSwapCompletedOnSuccess(t *testing.T) {
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
	events := &stubEventStore{}
	c := updateClient{
		Fetcher:    stubFetcher{Tag: "0.20.0"},
		Downloader: stubDownloader{Assets: map[string][]byte{asset: archive}},
		Current:    "0.19.0",
		BinaryPath: bin,
		ConfigPath: filepath.Join(dir, "omakase.yaml"),
		Validator:  validator.fn(),
		Backup:     &stubBackupRunner{path: "/tmp/backup.db"},
		EventStore: events,
	}
	if _, err := runUpdate(context.Background(), c, updateInputs{Yes: true}); err != nil {
		t.Fatalf("runUpdate: %v", err)
	}

	if got := events.countByType(domain.EventTypeUpdateHealthCheckPassed); got != 1 {
		t.Errorf("update.healthcheck.passed emissions = %d want 1", got)
	}
	if got := events.countByType(domain.EventTypeUpdateSwapCompleted); got != 1 {
		t.Errorf("update.swap.completed emissions = %d want 1", got)
	}
	if got := events.countByType(domain.EventTypeUpdateHealthCheckFailed); got != 0 {
		t.Errorf("update.healthcheck.failed emissions = %d want 0 on success path", got)
	}
	if got := events.countByType(domain.EventTypeUpdateSwapAborted); got != 0 {
		t.Errorf("update.swap.aborted emissions = %d want 0 on success path", got)
	}

	passed, _ := events.firstByType(domain.EventTypeUpdateHealthCheckPassed)
	if passed.payload["from_version"] != "0.19.0" || passed.payload["to_version"] != "0.20.0" {
		t.Errorf("passed payload from/to = %v/%v want 0.19.0/0.20.0", passed.payload["from_version"], passed.payload["to_version"])
	}
	completed, _ := events.firstByType(domain.EventTypeUpdateSwapCompleted)
	if completed.payload["binary_path"] != bin {
		t.Errorf("completed payload binary_path = %v want %s", completed.payload["binary_path"], bin)
	}
}

// TestRunUpdate_EmitsHealthCheckFailedAndSwapAbortedOnValidatorFail
// pins #369 AC 5 fail path: validator-fail emits both
// update.healthcheck.failed + update.swap.aborted exactly once, no
// healthcheck.passed, no swap.completed.
func TestRunUpdate_EmitsHealthCheckFailedAndSwapAbortedOnValidatorFail(t *testing.T) {
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
		OK:        false,
		Errors:    []map[string]any{{"kind": "theme_not_found", "message": "active theme nope not loadable"}},
		RawOutput: []byte(`{"ok":false}`),
	}}
	events := &stubEventStore{}
	c := updateClient{
		Fetcher:    stubFetcher{Tag: "0.20.0"},
		Downloader: stubDownloader{Assets: map[string][]byte{asset: archive}},
		Current:    "0.19.0",
		BinaryPath: bin,
		ConfigPath: filepath.Join(dir, "omakase.yaml"),
		Validator:  validator.fn(),
		Backup:     &stubBackupRunner{},
		EventStore: events,
	}
	if _, err := runUpdate(context.Background(), c, updateInputs{Yes: true}); err == nil {
		t.Fatalf("expected validator non-zero to abort update")
	}

	if got := events.countByType(domain.EventTypeUpdateHealthCheckFailed); got != 1 {
		t.Errorf("update.healthcheck.failed emissions = %d want 1", got)
	}
	if got := events.countByType(domain.EventTypeUpdateSwapAborted); got != 1 {
		t.Errorf("update.swap.aborted emissions = %d want 1", got)
	}
	if got := events.countByType(domain.EventTypeUpdateHealthCheckPassed); got != 0 {
		t.Errorf("update.healthcheck.passed emissions = %d want 0 on fail path", got)
	}
	if got := events.countByType(domain.EventTypeUpdateSwapCompleted); got != 0 {
		t.Errorf("update.swap.completed emissions = %d want 0 on fail path", got)
	}

	failed, _ := events.firstByType(domain.EventTypeUpdateHealthCheckFailed)
	if failed.payload["validator_first_error_kind"] != "theme_not_found" {
		t.Errorf("failed.validator_first_error_kind = %v want theme_not_found", failed.payload["validator_first_error_kind"])
	}
	aborted, _ := events.firstByType(domain.EventTypeUpdateSwapAborted)
	if aborted.payload["reason"] != "config_validation_failed" {
		t.Errorf("aborted.reason = %v want config_validation_failed", aborted.payload["reason"])
	}
}

// TestRunUpdate_ActivityWriteFailureDoesNotAbort pins #369 AC 3 + AC 6:
// when RecordEntityEvent itself returns an error, the update flow
// still completes the swap. The activity log row is best-effort; the
// swap's success criterion is the binary state.
func TestRunUpdate_ActivityWriteFailureDoesNotAbort(t *testing.T) {
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
	events := &stubEventStore{err: errors.New("activity store disk full")}
	c := updateClient{
		Fetcher:    stubFetcher{Tag: "0.20.0"},
		Downloader: stubDownloader{Assets: map[string][]byte{asset: archive}},
		Current:    "0.19.0",
		BinaryPath: bin,
		ConfigPath: filepath.Join(dir, "omakase.yaml"),
		Validator:  validator.fn(),
		Backup:     &stubBackupRunner{},
		EventStore: events,
	}
	res, err := runUpdate(context.Background(), c, updateInputs{Yes: true})
	if err != nil {
		t.Fatalf("runUpdate must not abort on activity write failure: %v", err)
	}
	payload, _ := res.(map[string]any)
	if payload["code"] != "update_completed" {
		t.Fatalf("code = %v want update_completed (graceful degrade)", payload["code"])
	}
	got, _ := os.ReadFile(bin)
	if string(got) != "NEW" {
		t.Fatalf("bin = %q want NEW (swap must still happen)", string(got))
	}
}

// TestEmitHealthCheckEvent_TruncatesValidatorRawExcerpt pins #369 AC 7:
// validator_raw_excerpt payloads above the 2 KB cap are truncated so
// the activity table doesn't bloat on broken bundles that produce
// multi-KB validator output.
func TestEmitHealthCheckEvent_TruncatesValidatorRawExcerpt(t *testing.T) {
	huge := strings.Repeat("x", healthCheckPayloadCap*3)
	events := &stubEventStore{}
	emitHealthCheckEvent(context.Background(), events, domain.EventTypeUpdateHealthCheckFailed, map[string]any{
		"validator_raw_excerpt": huge,
	})
	if len(events.calls) != 1 {
		t.Fatalf("calls = %d want 1", len(events.calls))
	}
	got, _ := events.calls[0].payload["validator_raw_excerpt"].(string)
	if len(got) != healthCheckPayloadCap {
		t.Fatalf("payload length = %d want %d (cap)", len(got), healthCheckPayloadCap)
	}
}

// TestEmitHealthCheckEvent_NilStoreNoOp pins the safety contract — a
// nil store must not panic so direct tests that don't care about
// emission can wire nil.
func TestEmitHealthCheckEvent_NilStoreNoOp(t *testing.T) {
	emitHealthCheckEvent(context.Background(), nil, "irrelevant", map[string]any{"foo": "bar"})
}
