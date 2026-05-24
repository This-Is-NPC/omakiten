// Package cliutil holds shared helpers consumed by both the cobra CLI
// (internal/cli) and the hook engine (internal/hooks/actions). The
// helpers stay free of i18n / domain typing so each caller wraps the
// outcome with the surface conventions of its own boundary.
package cliutil

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Sentinel errors returned by ResolveBinary. Callers compare with
// errors.Is to translate the failure stage into their surface format
// (i18n-aware coded error for the CLI editor; bare error string for
// the hook engine). The wrapped chain keeps the offending name + the
// underlying exec / filepath error so SafeError-style redaction still
// surfaces the actionable inner cause.
var (
	ErrBinaryEmpty           = errors.New("binary name is empty")
	ErrBinaryRelativeWithSep = errors.New("binary name is a relative path with embedded separator")
	ErrBinaryNotFoundOnPath  = errors.New("binary not found on PATH")
	ErrBinaryAbsResolveFail  = errors.New("absolute path resolution failed")
)

// ResolveBinary pins argv[0] to an absolute on-disk path so the spawn
// site cannot re-resolve the binary via PATH at fork time. Three input
// shapes are recognised:
//
//   - Absolute path → returned via filepath.Clean unchanged.
//   - Bare command name (no separator) → resolved via exec.LookPath +
//     filepath.Abs.
//   - Relative path with embedded separator ("./foo", "../bin/foo",
//     "sub/dir/foo") → rejected outright. The caller cannot reason
//     about the runtime's CWD, so silently resolving against it is a
//     footgun for both editor configuration and user-authored hooks.
//
// Empty / whitespace input returns ErrBinaryEmpty so the caller can
// surface a "not configured" message rather than passing through to
// LookPath and getting a misleading "command not found".
func ResolveBinary(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", ErrBinaryEmpty
	}
	if filepath.IsAbs(trimmed) {
		return filepath.Clean(trimmed), nil
	}
	if strings.ContainsRune(trimmed, filepath.Separator) {
		return "", fmt.Errorf("%w: %q", ErrBinaryRelativeWithSep, trimmed)
	}
	resolved, err := exec.LookPath(trimmed)
	if err != nil {
		return "", fmt.Errorf("%w: %q: %w", ErrBinaryNotFoundOnPath, trimmed, err)
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("%w: %q: %w", ErrBinaryAbsResolveFail, trimmed, err)
	}
	return abs, nil
}
