package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBackupLeaseRejectsLockPathReplacementAcrossServices(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows denies removal of the open lock path; Unix exercises pathname split detection")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "source.db")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "backups")
	first := NewBackupService(BackupOptions{SourcePath: source, DestDir: dest})
	second := NewBackupService(BackupOptions{SourcePath: source, DestDir: dest})
	secondEntered := false

	err := first.WithLease(context.Background(), func(firstLease BackupLease) error {
		lockPath := filepath.Join(dest, backupLeaseFilename)
		if err := os.Remove(lockPath); err != nil {
			return err
		}
		if err := os.WriteFile(lockPath, nil, 0o600); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		err := second.WithLease(ctx, func(BackupLease) error {
			secondEntered = true
			return nil
		})
		if !errors.Is(err, context.DeadlineExceeded) {
			return errors.New("replacement lock generation acquired while original callback remained active")
		}
		if secondEntered {
			return errors.New("second destructive callback entered after lock pathname replacement")
		}
		if err := firstLease.Validate(); err == nil {
			return errors.New("first destructive callback accepted a replaced lock pathname")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("first WithLease: %v", err)
	}
}

func TestBackupLeaseValidatePinsEveryGeneratedRecoveryPath(t *testing.T) {
	t.Parallel()

	tests := map[string]func(string) error{
		"removed": os.Remove,
		"replaced": func(path string) error {
			if err := os.Remove(path); err != nil {
				return err
			}
			return os.WriteFile(path, []byte("replacement"), 0o600)
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			source := filepath.Join(dir, "source.db")
			if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
				t.Fatal(err)
			}
			svc := NewBackupService(BackupOptions{SourcePath: source, DestDir: filepath.Join(dir, "backups")})
			if err := svc.WithLease(context.Background(), func(lease BackupLease) error {
				path, err := lease.Write(context.Background())
				if err != nil {
					return err
				}
				if err := mutate(path); err != nil {
					return err
				}
				if err := lease.Validate(); err == nil {
					return errors.New("lease accepted changed generated recovery path")
				}
				return nil
			}); err != nil {
				t.Fatalf("WithLease: %v", err)
			}
		})
	}
}

func TestBackupLeaseRejectsUnsafeExistingPOSIXDirectoryAndLockPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode checks do not establish Windows confidentiality; deployment must provide private native DACL inheritance")
	}

	dir := t.TempDir()
	source := filepath.Join(dir, "source.db")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, prepare := range map[string]func(string) error{
		"directory": func(dest string) error {
			if err := os.Mkdir(dest, 0o700); err != nil {
				return err
			}
			return os.Chmod(dest, 0o777)
		},
		"lock file": func(dest string) error {
			if err := os.Mkdir(dest, 0o700); err != nil {
				return err
			}
			path := filepath.Join(dest, backupLeaseFilename)
			if err := os.WriteFile(path, nil, 0o600); err != nil {
				return err
			}
			return os.Chmod(path, 0o666)
		},
	} {
		t.Run(name, func(t *testing.T) {
			dest := filepath.Join(dir, strings.ReplaceAll(name, " ", "-"))
			if err := prepare(dest); err != nil {
				t.Fatal(err)
			}
			svc := NewBackupService(BackupOptions{SourcePath: source, DestDir: dest})
			if err := svc.WithLease(context.Background(), func(BackupLease) error { return nil }); err == nil {
				t.Fatal("unsafe existing permissions were accepted")
			}
		})
	}
}

func TestQuarantineRemovalPreservesReplacementOnIdentityMismatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	const name = "2026-07-13T10-00-00.000000000Z.db"
	if err := root.WriteFile(name, []byte("expected"), 0o600); err != nil {
		t.Fatal(err)
	}
	expected, err := root.Lstat(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Remove(name); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile(name, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := quarantineAndRemoveBackup(root, name, expected, nil); err == nil {
		t.Fatal("quarantine removal accepted replacement inode")
	}
	body, err := root.ReadFile(name)
	if err != nil || string(body) != "replacement" {
		t.Fatalf("replacement was not preserved: body=%q err=%v", body, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".omakiten-prune-") {
			t.Fatalf("identity mismatch left quarantine link %q", entry.Name())
		}
	}
}

func TestQuarantineRemovalNeverUnlinksRetainedBackupAfterSwap(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	const retainedName = "2026-07-13T10-00-01.000000000Z.db"
	const candidateName = "2026-07-13T10-00-00.000000000Z.db"
	if err := root.WriteFile(retainedName, []byte("retained"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile(candidateName, []byte("candidate"), 0o600); err != nil {
		t.Fatal(err)
	}
	retained, err := root.Lstat(retainedName)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := root.Lstat(candidateName)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Remove(candidateName); err != nil {
		t.Fatal(err)
	}
	if err := root.Link(retainedName, candidateName); err != nil {
		t.Fatal(err)
	}
	if err := quarantineAndRemoveBackup(root, candidateName, candidate, retained); err == nil {
		t.Fatal("quarantine removal accepted retained-inode swap")
	}
	for _, name := range []string{retainedName, candidateName} {
		info, err := root.Lstat(name)
		if err != nil || !os.SameFile(retained, info) {
			t.Fatalf("retained identity at %s changed: info=%v err=%v", name, info, err)
		}
	}
}
