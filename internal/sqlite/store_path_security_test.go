package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"omakiten/internal/domain"
)

func TestOpenRejectsSymlinkedDatabaseParentWithoutMutation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	realPath := filepath.Join(realDir, "omakiten.db")
	seedCurrentDatabase(t, ctx, realPath)
	before := schemaFileSnapshot(t, realPath)
	linkedDir := filepath.Join(root, "linked")
	if err := os.Symlink(realDir, linkedDir); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	_, err := Open(ctx, filepath.Join(linkedDir, "omakiten.db"))
	assertDatabaseValidationError(t, err)
	if after := schemaFileSnapshot(t, realPath); after != before {
		t.Fatalf("symlinked-parent rejection mutated target:\nbefore=%s\nafter=%s", before, after)
	}
}

func TestOpenRejectsSymlinkedDatabaseTargetWithoutMutation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	realPath := filepath.Join(dir, "real.db")
	seedCurrentDatabase(t, ctx, realPath)
	before := schemaFileSnapshot(t, realPath)
	linkPath := filepath.Join(dir, "omakiten.db")
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	_, err := Open(ctx, linkPath)
	assertDatabaseValidationError(t, err)
	if after := schemaFileSnapshot(t, realPath); after != before {
		t.Fatalf("symlinked-target rejection mutated target:\nbefore=%s\nafter=%s", before, after)
	}
}

func TestOpenRejectsReplacementDuringLazyOpenWithoutMutation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "omakiten.db")
	seedCurrentDatabase(t, ctx, path)
	originalPath := path + ".original"
	originalBefore := schemaFileSnapshot(t, path)

	var replacementBefore string
	var hookErr error
	opened, err := openWithOptions(ctx, path, Options{}, func() {
		if hookErr = os.Rename(path, originalPath); hookErr != nil {
			return
		}
		replacement, openErr := sql.Open("sqlite", path)
		if openErr != nil {
			hookErr = openErr
			return
		}
		defer func() { hookErr = errors.Join(hookErr, replacement.Close()) }()
		if _, hookErr = replacement.ExecContext(ctx, schemaSQL); hookErr != nil {
			return
		}
		if _, hookErr = replacement.ExecContext(ctx, "PRAGMA journal_mode = DELETE"); hookErr != nil {
			return
		}
		replacementBefore = schemaFileSnapshot(t, path)
	})
	if hookErr != nil {
		t.Fatalf("replace database during lazy open: %v", hookErr)
	}
	if opened != nil {
		_ = opened.Close()
		t.Fatal("Open accepted a replaced database during lazy open")
	}
	assertDatabaseValidationError(t, err)
	if after := schemaFileSnapshot(t, path); after != replacementBefore {
		t.Fatalf("replacement target mutated before rejection:\nbefore=%s\nafter=%s", replacementBefore, after)
	}
	if after := schemaFileSnapshot(t, originalPath); after != originalBefore {
		t.Fatalf("original database changed during replacement:\nbefore=%s\nafter=%s", originalBefore, after)
	}
}

func assertDatabaseValidationError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("Open succeeded, want validation error")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrValidation {
		t.Fatalf("Open error = %v, want validation_error", err)
	}
}
