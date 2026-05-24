package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadFileBoundedRejectsOversize pins the io.LimitReader-based
// cap: a file one byte over the cap surfaces a coded ErrConfigTooLarge.
func TestReadFileBoundedRejectsOversize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "oversize.txt")
	payload := strings.Repeat("x", 1025)
	if err := writeFileForTest(t, path, payload); err != nil {
		t.Fatalf("writeFileForTest: %v", err)
	}
	_, err := readFileBounded(path, 1024)
	if err == nil {
		t.Fatalf("expected ErrConfigTooLarge, got nil")
	}
	if !IsConfigTooLarge(err) {
		t.Fatalf("expected coded ErrConfigTooLarge, got %T %v", err, err)
	}
}

// TestReadFileBoundedAcceptsAtCap proves the cap is inclusive: a file
// exactly at the cap reads cleanly; only > cap trips the guard.
func TestReadFileBoundedAcceptsAtCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "at-cap.txt")
	payload := strings.Repeat("x", 1024)
	if err := writeFileForTest(t, path, payload); err != nil {
		t.Fatalf("writeFileForTest: %v", err)
	}
	got, err := readFileBounded(path, 1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(got, []byte(payload)) {
		t.Fatalf("payload round-trip mismatch")
	}
}

// FuzzReadBoundedDoesNotPanicOnOverflow exercises the LimitReader
// guard across a wide range of inputs sized around the cap. Any input
// that overflows must return the coded error without panicking; any
// input within the cap must round-trip the first len(data) bytes.
func FuzzReadBoundedDoesNotPanicOnOverflow(f *testing.F) {
	f.Add([]byte(""), int64(16))
	f.Add([]byte("abc"), int64(2))
	f.Add(bytes.Repeat([]byte("x"), 32), int64(32))
	f.Add(bytes.Repeat([]byte("x"), 33), int64(32))

	f.Fuzz(func(t *testing.T, data []byte, max int64) {
		if max < 0 || max > int64(1<<20) {
			return
		}
		got, err := readBounded(bytes.NewReader(data), "fuzz", max)
		if int64(len(data)) > max {
			if err == nil {
				t.Fatalf("overflow with len=%d max=%d returned no error", len(data), max)
			}
			if !IsConfigTooLarge(err) {
				t.Fatalf("overflow returned non-coded error: %T %v", err, err)
			}
			return
		}
		if err != nil {
			t.Fatalf("under-cap input errored: %v (len=%d max=%d)", err, len(data), max)
		}
		if !bytes.Equal(got, data) {
			t.Fatalf("under-cap round-trip drift: got %d bytes, want %d", len(got), len(data))
		}
	})
}

// writeFileForTest is a sparse helper kept out of the production tree.
func writeFileForTest(t *testing.T, path, body string) error {
	t.Helper()
	return os.WriteFile(path, []byte(body), 0o600)
}
