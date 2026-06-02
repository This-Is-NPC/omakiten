package config

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
)

// WriteAtomic writes data to path through a temp file + rename, ensuring no
// reader observes a half-written file.
func WriteAtomic(path string, data []byte) error {
	// Create the parent dir with 0o700 (owner-only) to match the 0o600 config
	// files we write into it: world-listable parents would leak the presence of
	// those files to other users on shared machines.
	//
	// Known limitation: os.MkdirAll is not atomic. On network filesystems or
	// under concurrent invocation, partial directory state is observable, and an
	// existing parent dir keeps whatever mode it already has (MkdirAll does not
	// chmod it). Acceptable for a local single-user CLI; revisit with a
	// check-then-create + lock-file pattern if the threat model expands to
	// multi-user or containerized environments.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := io.Copy(tmp, bytes.NewReader(data)); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}
