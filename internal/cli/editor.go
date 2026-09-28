package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"omakiten/internal/cliutil"
	"omakiten/internal/domain"
)

// openEditorAndReimport runs $EDITOR (or the user-resolved editor) against
// path, then re-loads + re-imports the bundle so the materialized SQLite store
// reflects whatever the user wrote.
func openEditorAndReimport(ctx context.Context, rt *runtime, path string) error {
	if path == "" {
		return nil
	}
	if err := runEditorCommand(path); err != nil {
		return domain.NewError(domain.ErrEditorFailed, err.Error(), map[string]any{"path": path})
	}
	return rt.operationService().ReimportBundle(ctx)
}

func runEditorCommand(path string) error {
	editor := cliutil.ResolveEditor()
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		return domain.NewError(domain.ErrEditorNotFound, t("cli.editor.not_configured"), nil)
	}
	resolved, err := resolveEditorBinary(parts[0])
	if err != nil {
		return err
	}
	args := append(parts[1:], path)
	cmd := exec.Command(resolved, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor %q exited: %w", editor, err)
	}
	return nil
}

// resolveEditorBinary pins the editor's argv[0] to an absolute on-disk
// path via the shared cliutil.ResolveBinary helper. Empty input maps to
// the "not configured" i18n key; every other failure (relative-with-
// separator, missing on PATH, abs lookup failure) collapses to the
// "not found" i18n key. The bare cliutil error chain is preserved on
// the details.error field via SafeError so agent surfaces still get
// the actionable inner cause without the path-bearing wrap prefix.
func resolveEditorBinary(name string) (string, error) {
	resolved, err := cliutil.ResolveBinary(name)
	if err == nil {
		return resolved, nil
	}
	if errors.Is(err, cliutil.ErrBinaryEmpty) {
		return "", domain.NewError(domain.ErrEditorNotFound, t("cli.editor.not_configured"), nil)
	}
	return "", domain.NewError(domain.ErrEditorNotFound, t("cli.editor.not_found"), map[string]any{"editor": strings.TrimSpace(name), "error": domain.SafeError(err)})
}
