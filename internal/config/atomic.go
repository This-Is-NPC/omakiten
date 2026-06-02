package config

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
)

// WriteAtomic writes data to path through a temp file + rename, ensuring no
// reader observes a half-written file.
//
// WriteAtomic is a generic primitive: it serves both omakiten-owned config
// paths (the config root and its entity subtrees) and foreign harness paths
// such as ~/.claude/.mcp.json or an arbitrary --config-path target. It must
// therefore stay neutral about the parent directory's mode and never chmod a
// directory it did not create — clobbering the mode of ~/.claude/ (shared with
// Claude Code) or a user-chosen --config-path parent would be both surprising
// and a source of install/config-write errors.
//
// Parent-dir hardening therefore lives where omakiten owns the directory tree
// (see hardenDir / EnsureDefaultFiles in default_files.go), not here.
func WriteAtomic(path string, data []byte) error {
	// Best-effort: when omakiten is the one creating a brand-new parent dir, we
	// create it owner-only (0o700) to match the 0o600 files we write into it.
	//
	// Scope of this hardening is narrow and honest: os.MkdirAll only applies
	// 0o700 to directories it actually creates. A pre-existing parent (the
	// common case for ~/.claude/, created by Claude Code at ~0o755) keeps its
	// current mode — MkdirAll does not chmod it, and we deliberately do not
	// chmod it either (see the type-level comment). So the file-presence leak is
	// closed only for omakiten-created dirs on first write; for shared,
	// pre-existing parents it stays open by design.
	//
	// Known limitation: os.MkdirAll is also not atomic. On network filesystems
	// or under concurrent invocation, partial directory state is observable.
	// Acceptable for a local single-user CLI; revisit with a check-then-create +
	// lock-file pattern if the threat model expands to multi-user or
	// containerized environments.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	// Register temp-file cleanup immediately after creation, before any operation
	// that can fail (e.g. the Chmod below). Otherwise an early-return error path
	// would orphan the temp file in the config dir.
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	// Enforce 0o600 explicitly rather than relying on os.CreateTemp's implicit
	// 0o600 surviving the rename. The invariant — config files are owner-only —
	// is then guaranteed by this call, not inherited from a runtime default that
	// a future refactor could silently change.
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}

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
