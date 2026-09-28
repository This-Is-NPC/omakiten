package recovery

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"omakiten/internal/contract"
)

func TestBackupLeaseHonorsContextWhileAnotherHandleOwnsLock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := filepath.Join(dir, "source.db")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "backups")
	first := newTestBackupService(BackupOptions{SourcePath: source, DestDir: dest})
	second := newTestBackupService(BackupOptions{SourcePath: source, DestDir: dest})
	locked := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- first.WithLease(context.Background(), func(contract.BackupLease) error {
			close(locked)
			<-release
			return nil
		})
	}()
	<-locked

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := second.WithLease(ctx, func(contract.BackupLease) error {
		return errors.New("second lease unexpectedly acquired")
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second WithLease error = %v, want context deadline", err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first WithLease: %v", err)
	}
}

func TestBackupPruneRejectsReplacedDirectoryAndAttackerTimestampFile(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not permit renaming this test's open directory handle")
	}
	source, dest, moved, attackerPath, originalNames := backupPruneReplacementFixture(t)
	svc := newTestBackupService(BackupOptions{SourcePath: source, DestDir: dest, Retention: 1})
	if err := svc.WithLease(context.Background(), func(lease contract.BackupLease) error {
		return replaceBackupDirectoryAndRejectPrune(lease, dest, moved, filepath.Dir(attackerPath))
	}); err != nil {
		t.Fatalf("WithLease: %v", err)
	}
	if body, err := os.ReadFile(attackerPath); err != nil || string(body) != "unrelated" {
		t.Fatalf("attacker timestamp file changed: body=%q err=%v", body, err)
	}
	for _, name := range originalNames {
		if _, err := os.Stat(filepath.Join(moved, name)); err != nil {
			t.Fatalf("original backup %s was removed after replacement: %v", name, err)
		}
	}
}

func backupPruneReplacementFixture(t *testing.T) (string, string, string, string, []string) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.db")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "backups")
	if err := os.Mkdir(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	originalNames := []string{
		"2026-07-13T10-00-00.000000000Z.db",
		"2026-07-13T10-00-01.000000000Z.db",
	}
	for _, name := range originalNames {
		if err := os.WriteFile(filepath.Join(dest, name), []byte("original"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	attacker := filepath.Join(dir, "attacker")
	if err := os.Mkdir(attacker, 0o700); err != nil {
		t.Fatal(err)
	}
	attackerPath := filepath.Join(attacker, "2026-07-13T10-00-02.000000000Z.db")
	if err := os.WriteFile(attackerPath, []byte("unrelated"), 0o600); err != nil {
		t.Fatal(err)
	}
	return source, dest, filepath.Join(dir, "original-backups"), attackerPath, originalNames
}

func replaceBackupDirectoryAndRejectPrune(lease contract.BackupLease, dest, moved, attacker string) error {
	if err := os.Rename(dest, moved); err != nil {
		return err
	}
	if err := os.Symlink(attacker, dest); err != nil {
		return err
	}
	if err := lease.PruneRetaining(""); err == nil {
		return errors.New("prune accepted a replaced backup directory")
	}
	return nil
}

func TestBackupFailedPruneBoundsDisabledRetentionAndProtectsRecovery(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := filepath.Join(dir, "source.db")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "backups")
	if err := os.Mkdir(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	paths := []string{
		filepath.Join(dest, "2026-07-13T10-00-00.000000000Z.db"),
		filepath.Join(dest, "2026-07-13T10-00-01.000000000Z.db"),
		filepath.Join(dest, "2026-07-13T10-00-02.000000000Z.db"),
	}
	for index, path := range paths {
		if err := os.WriteFile(path, []byte{byte(index)}, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	svc := newTestBackupService(BackupOptions{SourcePath: source, DestDir: dest, Retention: 0})
	if err := svc.WithLease(context.Background(), func(lease contract.BackupLease) error {
		return lease.PruneFailedRetaining(paths[0])
	}); err != nil {
		t.Fatalf("PruneFailedRetaining: %v", err)
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatal(err)
	}
	entries = matchingBackupEntries(entries)
	if len(entries) != 1 || entries[0].Name() != filepath.Base(paths[0]) {
		t.Fatalf("failed-operation backups = %v, want only protected %q", entries, filepath.Base(paths[0]))
	}
}
