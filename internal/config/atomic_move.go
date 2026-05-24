package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
)

// atomicMove relocates src to dst atomically. The fast path is a
// straight os.Rename which the kernel guarantees is atomic when both
// paths live on the same filesystem; concurrent readers either see
// the old name or the new one, never an intermediate state. When the
// rename fails with EXDEV (cross-filesystem), the helper falls back
// to copy → fsync → rename-to-final → remove-source so the
// destination still flips atomically (the rename on the dst FS is the
// commit point) while the src lingers only until cleanup.
//
// Used by MigrateLayout to keep `okt install --refresh` from exposing
// a partial config tree to a concurrent TUI hot-reload that races the
// migration loop.
func atomicMove(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !isCrossDeviceError(err) {
		return fmt.Errorf("rename %s -> %s: %w", src, dst, err)
	}
	// Cross-filesystem fallback. Stage on the destination FS via a
	// hidden temp file so the final rename is atomic; remove the
	// source only after the rename commits so a crash mid-copy leaves
	// both files in a recoverable state.
	tmp := dst + ".migrate.tmp"
	if err := copyFileBytes(src, tmp); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("copy %s -> %s: %w", src, tmp, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, dst, err)
	}
	if err := os.Remove(src); err != nil {
		return fmt.Errorf("remove source %s after cross-fs move: %w", src, err)
	}
	return nil
}

func copyFileBytes(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = srcFile.Close() }()
	info, err := srcFile.Stat()
	if err != nil {
		return err
	}
	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(dstFile, srcFile); err != nil {
		_ = dstFile.Close()
		return err
	}
	if err := dstFile.Sync(); err != nil {
		_ = dstFile.Close()
		return err
	}
	return dstFile.Close()
}

// isCrossDeviceError reports whether err is the EXDEV signal os.Rename
// emits when src + dst live on different filesystems. Wrapped through
// errors.Is so wrapped syscall errors are still recognised.
func isCrossDeviceError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EXDEV) {
		return true
	}
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return errors.Is(linkErr.Err, syscall.EXDEV)
	}
	return false
}
