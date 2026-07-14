//go:build windows

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsSnapshotPublicationHasNoUnsupportedDirectoryFlushError(t *testing.T) {
	if err := syncPublishedSnapshotDirectory(nil); err != nil {
		t.Fatalf("Windows post-publication directory durability returned error: %v", err)
	}
	parent := t.TempDir()
	dir := filepath.Join(parent, "stage")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	stagePath := filepath.Join(dir, "snapshot.db")
	staged, err := os.OpenFile(stagePath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		_ = staged.Close()
		t.Fatal(err)
	}
	stageRoot, err := root.OpenRoot("stage")
	if err != nil {
		_ = root.Close()
		_ = staged.Close()
		t.Fatal(err)
	}
	stageDirectory, err := stageRoot.Open(".")
	if err != nil {
		_ = stageRoot.Close()
		_ = root.Close()
		_ = staged.Close()
		t.Fatal(err)
	}
	path, _, release, err := bindSnapshotStage(staged, stageDirectory, stagePath)
	if err != nil || path != stagePath {
		_ = stageDirectory.Close()
		_ = stageRoot.Close()
		_ = root.Close()
		_ = staged.Close()
		t.Fatalf("Windows staging binding = %q, %v; want %q", path, err, stagePath)
	}
	if err := os.Rename(stagePath, stagePath+".replaced"); err == nil {
		_ = release()
		_ = staged.Close()
		t.Fatal("Windows staging binding allowed rename while SQLite verification pathname was active")
	}
	if err := release(); err != nil {
		_ = staged.Close()
		t.Fatalf("release staging binding: %v", err)
	}
	if err := staged.Close(); err != nil {
		_ = stageDirectory.Close()
		t.Fatal(err)
	}
	if err := stageDirectory.Close(); err != nil {
		_ = stageRoot.Close()
		_ = root.Close()
		t.Fatal(err)
	}
	if err := stageRoot.Close(); err != nil {
		_ = root.Close()
		t.Fatal(err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(stagePath, stagePath+".released"); err != nil {
		t.Fatalf("stage remained rename-blocked after release: %v", err)
	}
}

func TestWindowsSnapshotReachesVacuumAndVerificationWithFileURI(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	extendedSourcePath := `\\?\` + sourcePath
	db, err := sql.Open("sqlite", sqliteFileURI(extendedSourcePath, "mode=rwc"))
	if err != nil {
		t.Fatalf("open Windows source URI: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE marker(value TEXT); INSERT INTO marker VALUES ('verified')`); err != nil {
		_ = db.Close()
		t.Fatalf("initialize Windows source URI: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	afterVacuum := false
	destinationPath := filepath.Join(dir, "snapshot.db")
	err = snapshotDatabaseWithOptions(ctx, extendedSourcePath, destinationPath, false, snapshotHooks{
		AfterVacuum: func() { afterVacuum = true },
	})
	if err != nil {
		t.Fatalf("Windows snapshot failed before verified publication: %v", err)
	}
	if !afterVacuum {
		t.Fatal("Windows snapshot returned without reaching VACUUM")
	}
	snapshot, err := sql.Open("sqlite", sqliteFileURI(destinationPath, "mode=ro"))
	if err != nil {
		t.Fatalf("open Windows snapshot URI: %v", err)
	}
	defer func() { _ = snapshot.Close() }()
	var value string
	if err := snapshot.QueryRowContext(ctx, `SELECT value FROM marker`).Scan(&value); err != nil || value != "verified" {
		t.Fatalf("Windows verified snapshot value = %q, %v", value, err)
	}
}

func TestWindowsSnapshotStageBindingRejectsMismatchedOriginalHandle(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "stage")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	originalPath := filepath.Join(dir, "original.db")
	original, err := os.OpenFile(originalPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = original.Close() }()

	replacementPath := filepath.Join(dir, "replacement.db")
	if err := os.WriteFile(replacementPath, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	stageRoot, err := root.OpenRoot("stage")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stageRoot.Close() }()
	stageDirectory, err := stageRoot.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stageDirectory.Close() }()
	_, _, release, err := bindSnapshotStage(original, stageDirectory, replacementPath)
	if err == nil {
		if release != nil {
			_ = release()
		}
		t.Fatal("Windows staging binding accepted a pathname for a different file identity")
	}
	if !strings.Contains(err.Error(), "identity") {
		t.Fatalf("Windows staging identity mismatch error = %q", err)
	}
	if release != nil {
		t.Fatal("Windows staging identity mismatch returned a release callback")
	}
	if err := os.Rename(replacementPath, replacementPath+".released"); err != nil {
		t.Fatalf("mismatched Windows staging handle remained open: %v", err)
	}
}

func TestWindowsSnapshotStageCannotBeLexicallyReplacedDuringVacuum(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	db, err := sql.Open("sqlite", sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE marker(value TEXT)`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	_ = db.Close()
	var replacementErr error
	destinationPath := filepath.Join(dir, "snapshot.db")
	preservedStagePath := filepath.Join(dir, "preserved-stage.db")
	err = snapshotDatabaseWithOptions(ctx, sourcePath, destinationPath, false, snapshotHooks{
		AfterStageCreated: func(_, stagePath string) {
			replacementErr = os.Rename(stagePath, preservedStagePath)
		},
	})
	if replacementErr != nil {
		if !errors.Is(replacementErr, os.ErrPermission) {
			t.Logf("Windows denied stage replacement with platform error: %v", replacementErr)
		}
		if err != nil {
			t.Fatalf("snapshot failed after denied stage replacement: %v", err)
		}
		return
	}
	if err == nil {
		if err := os.WriteFile(preservedStagePath, []byte("original verified stage"), 0o600); err != nil {
			t.Fatal(err)
		}
		if body, readErr := os.ReadFile(destinationPath); readErr != nil || string(body) != "original verified stage" {
			t.Fatalf("allowed Windows stage race published replacement inode: body=%q read=%v", body, readErr)
		}
		return
	}
	if _, statErr := os.Lstat(destinationPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("rejected Windows stage race left destination: %v (snapshot error: %v)", statErr, err)
	}
}
