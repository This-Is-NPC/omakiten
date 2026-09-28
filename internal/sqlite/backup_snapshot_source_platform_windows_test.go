//go:build windows

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsSnapshotSourcePinRejectsNestedReparseAncestor(t *testing.T) {
	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	database := filepath.Join(link, "nested", "source.db")
	if err := os.MkdirAll(filepath.Dir(database), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(database, nil, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := pinSnapshotSource(database); err == nil {
		t.Fatal("pinSnapshotSource accepted a nested reparse ancestor")
	} else if err := SnapshotDatabase(context.Background(), database, filepath.Join(root, "out.db")); err == nil {
		t.Fatal("SnapshotDatabase accepted a nested reparse source ancestor")
	}
}

func TestWindowsSnapshotSourcePinRejectsReparseSidecars(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	database := filepath.Join(parent, "source.db")
	if err := os.WriteFile(database, nil, 0o600); err != nil {
		t.Fatalf("WriteFile database: %v", err)
	}
	realSidecar := filepath.Join(parent, "outside-wal")
	if err := os.WriteFile(realSidecar, []byte("outside"), 0o600); err != nil {
		t.Fatalf("WriteFile sidecar: %v", err)
	}
	if err := os.Symlink(realSidecar, database+"-wal"); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if _, err := pinSnapshotSource(database); err == nil {
		t.Fatal("pinSnapshotSource accepted a reparse sidecar")
	} else if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pinSnapshotSource returned raw not-exist error: %v", err)
	}
}

func TestWindowsSnapshotSourcePinAcceptsRegularSource(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	db, err := sql.Open("sqlite", sourcePath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE marker(value TEXT); INSERT INTO marker VALUES ('verified')`); err != nil {
		_ = db.Close()
		t.Fatalf("initialize source: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	destinationPath := filepath.Join(dir, "snapshot.db")
	if err := SnapshotDatabase(ctx, sourcePath, destinationPath); err != nil {
		t.Fatalf("SnapshotDatabase with regular source: %v", err)
	}
	snapshot, err := sql.Open("sqlite", destinationPath)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	defer func() { _ = snapshot.Close() }()
	var value string
	if err := snapshot.QueryRowContext(ctx, `SELECT value FROM marker`).Scan(&value); err != nil || value != "verified" {
		t.Fatalf("snapshot value = %q, %v", value, err)
	}
}
