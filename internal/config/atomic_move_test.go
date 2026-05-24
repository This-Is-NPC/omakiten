package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestAtomicMoveSameFilesystemUsesRename pins the fast path: when src
// and dst live on the same filesystem, atomicMove must dispatch a
// single os.Rename (no copy-and-remove). The proof is that the src
// file's inode survives the move when the underlying syscall is
// rename — the destination's inode equals the source's pre-move
// inode.
func TestAtomicMoveSameFilesystemUsesRename(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("payload"), 0o600); err != nil {
		t.Fatalf("seed src: %v", err)
	}
	srcInfo, err := os.Stat(src)
	if err != nil {
		t.Fatalf("stat src: %v", err)
	}
	if err := atomicMove(src, dst); err != nil {
		t.Fatalf("atomicMove: %v", err)
	}
	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("src should be gone, got %v", err)
	}
	dstInfo, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat dst: %v", err)
	}
	if !sameInode(srcInfo, dstInfo) {
		t.Fatalf("same-fs move did not preserve inode; copy path took over instead of rename")
	}
	body, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if !bytes.Equal(body, []byte("payload")) {
		t.Fatalf("payload mismatch after move: %q", body)
	}
}

// TestAtomicMoveOverExistingDstReplacesContent confirms the destination
// is overwritten when a same-named target exists — same semantics as
// os.Rename so callers do not need to pre-clean the destination.
func TestAtomicMoveOverExistingDstReplacesContent(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("new"), 0o600); err != nil {
		t.Fatalf("seed src: %v", err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0o600); err != nil {
		t.Fatalf("seed dst: %v", err)
	}
	if err := atomicMove(src, dst); err != nil {
		t.Fatalf("atomicMove: %v", err)
	}
	body, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if !bytes.Equal(body, []byte("new")) {
		t.Fatalf("dst not replaced; got %q want %q", body, "new")
	}
}

// TestIsCrossDeviceErrorRecognisesEXDEV is a tight unit on the EXDEV
// detection helper — wraps the raw syscall errno in an os.LinkError
// (the shape os.Rename returns) and asserts isCrossDeviceError flips.
func TestIsCrossDeviceErrorRecognisesEXDEV(t *testing.T) {
	if !isCrossDeviceError(&os.LinkError{Op: "rename", Old: "a", New: "b", Err: syscall.EXDEV}) {
		t.Fatalf("isCrossDeviceError did not detect EXDEV inside os.LinkError")
	}
	if isCrossDeviceError(&os.LinkError{Op: "rename", Old: "a", New: "b", Err: syscall.ENOENT}) {
		t.Fatalf("isCrossDeviceError matched ENOENT (false positive)")
	}
	if isCrossDeviceError(nil) {
		t.Fatalf("isCrossDeviceError matched nil")
	}
}

// TestCopyFileBytesPreservesContentAndPermissions exercises the
// cross-fs fallback's copyFileBytes helper in isolation. Same-fs
// tests cannot reach the copy branch (the rename path wins), so the
// helper's correctness is asserted directly here.
func TestCopyFileBytesPreservesContentAndPermissions(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	dst := filepath.Join(dir, "dst.bin")
	payload := bytes.Repeat([]byte("k"), 4096)
	if err := os.WriteFile(src, payload, 0o640); err != nil {
		t.Fatalf("seed src: %v", err)
	}
	if err := copyFileBytes(src, dst); err != nil {
		t.Fatalf("copyFileBytes: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch")
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat dst: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o640 {
		t.Fatalf("dst perm = %o, want 640", perm)
	}
}
