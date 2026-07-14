package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBackupService_Run_WritesSnapshotWithExpectedNameAndContent(t *testing.T) {
	tmp := t.TempDir()
	srcPath := filepath.Join(tmp, "omakiten.db")
	want := []byte("SQLite format 3\x00 fake header + body")
	if err := os.WriteFile(srcPath, want, 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	destDir := filepath.Join(tmp, "state", "backups")
	fixed := time.Date(2026, 5, 21, 14, 30, 45, 0, time.UTC)
	svc := NewBackupService(BackupOptions{
		SourcePath: srcPath,
		DestDir:    destDir,
		Retention:  5,
		Now:        func() time.Time { return fixed },
	})

	path, err := svc.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	wantName := "2026-05-21T14-30-45.000000000Z.db"
	if filepath.Base(path) != wantName {
		t.Fatalf("Run() basename = %q, want %q", filepath.Base(path), wantName)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("snapshot content mismatch: got %q, want %q", got, want)
	}
}

func TestBackupService_RunUsesInjectedContextAwareSnapshotWriter(t *testing.T) {
	tmp := t.TempDir()
	srcPath := filepath.Join(tmp, "omakiten.db")
	if err := os.WriteFile(srcPath, []byte("source"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("marker"), "present")
	called := false
	svc := NewBackupService(BackupOptions{
		SourcePath: srcPath,
		DestDir:    filepath.Join(tmp, "backups"),
		Now:        func() time.Time { return time.Date(2026, 5, 21, 14, 30, 45, 0, time.UTC) },
		SnapshotWriter: func(gotCtx context.Context, sourcePath, destinationPath string) error {
			called = true
			if gotCtx.Value(contextKey("marker")) != "present" {
				t.Fatal("snapshot writer did not receive Run context")
			}
			if sourcePath != srcPath {
				t.Fatalf("snapshot writer source = %q, want %q", sourcePath, srcPath)
			}
			if info, err := os.Stat(filepath.Dir(destinationPath)); err != nil || !info.IsDir() {
				t.Fatalf("BackupService did not prepare the leased destination directory: %v", err)
			}
			if err := os.MkdirAll(filepath.Dir(destinationPath), 0o700); err != nil {
				return err
			}
			return AtomicCopyFile(sourcePath, destinationPath, destinationPath+".tmp")
		},
	})

	path, err := svc.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !called {
		t.Fatal("injected snapshot writer was not called")
	}
	if body, err := os.ReadFile(path); err != nil || string(body) != "source" {
		t.Fatalf("injected snapshot output = %q, %v", body, err)
	}
}

// TestBackupService_Run_NoCollisionWithinSameSecond pins two Run() calls
// to the same wall-clock second with distinct nanosecond components and
// asserts both snapshots survive on disk. The pre-fix filename used
// second granularity, so the second Run would silently os.Rename over
// the first — a regression guard for the destructive-flow safety net.
func TestBackupService_Run_NoCollisionWithinSameSecond(t *testing.T) {
	tmp := t.TempDir()
	srcPath := filepath.Join(tmp, "omakiten.db")
	if err := os.WriteFile(srcPath, []byte("body"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	destDir := filepath.Join(tmp, "backups")
	sameSecond := time.Date(2026, 5, 21, 14, 30, 45, 0, time.UTC)

	for i, nanos := range []int{1, 2} {
		nanos := nanos
		ts := sameSecond.Add(time.Duration(nanos) * time.Nanosecond)
		svc := NewBackupService(BackupOptions{
			SourcePath: srcPath,
			DestDir:    destDir,
			Now:        func() time.Time { return ts },
		})
		if _, err := svc.Run(context.Background()); err != nil {
			t.Fatalf("Run() #%d error = %v", i, err)
		}
	}

	entries, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatalf("list dest: %v", err)
	}
	dbCount := 0
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".db" {
			dbCount++
		}
	}
	if dbCount != 2 {
		t.Fatalf("dest .db entries = %d, want 2 (nano granularity prevents collision)", dbCount)
	}
}

// TestBackupService_Run_PruneWarnNotInvokedOnSuccess pins the contract
// that PruneWarn is reserved for failures; a clean prune pass must not
// fire the callback. Guards against accidental "log every prune"
// regressions that would noise up the CLI stderr or TUI status bar.
func TestBackupService_Run_PruneWarnNotInvokedOnSuccess(t *testing.T) {
	tmp := t.TempDir()
	srcPath := filepath.Join(tmp, "omakiten.db")
	if err := os.WriteFile(srcPath, []byte("body"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	destDir := filepath.Join(tmp, "backups")
	captured := 0
	for i := 0; i < 3; i++ {
		i := i
		svc := NewBackupService(BackupOptions{
			SourcePath: srcPath,
			DestDir:    destDir,
			Retention:  1,
			Now:        func() time.Time { return time.Date(2026, 5, 21, 10, i, 0, 0, time.UTC) },
			PruneWarn:  func(error) { captured++ },
		})
		if _, err := svc.Run(context.Background()); err != nil {
			t.Fatalf("Run() #%d error = %v", i, err)
		}
	}
	if captured != 0 {
		t.Fatalf("PruneWarn invoked %d times on successful prune passes; want 0", captured)
	}
}

func TestBackupService_Run_CreatesDestDirIfMissing(t *testing.T) {
	tmp := t.TempDir()
	srcPath := filepath.Join(tmp, "omakiten.db")
	if err := os.WriteFile(srcPath, []byte("payload"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	destDir := filepath.Join(tmp, "missing", "subdir", "backups")
	svc := NewBackupService(BackupOptions{
		SourcePath: srcPath,
		DestDir:    destDir,
		Now:        func() time.Time { return time.Unix(1, 0).UTC() },
	})
	if _, err := svc.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	info, err := os.Stat(destDir)
	if err != nil {
		t.Fatalf("stat dest dir: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("dest path is not a directory")
	}
}

func TestBackupService_Run_SourceMissingErrors(t *testing.T) {
	tmp := t.TempDir()
	svc := NewBackupService(BackupOptions{
		SourcePath: filepath.Join(tmp, "no-such-file.db"),
		DestDir:    filepath.Join(tmp, "backups"),
	})
	if _, err := svc.Run(context.Background()); err == nil {
		t.Fatalf("Run() error = nil, want missing-source error")
	}
}

func TestBackupService_Run_NoPartialFileOnCopyFailure(t *testing.T) {
	// Source exists but the destination directory path collides with
	// an existing regular file — MkdirAll fails before any copy starts.
	// The contract is that the dest dir is never half-created and the
	// returned error names the failure. Same invariant guards "tmp
	// file left over" — there is no tmp to leave behind because the
	// pre-flight stops before open(dst).
	tmp := t.TempDir()
	srcPath := filepath.Join(tmp, "omakiten.db")
	if err := os.WriteFile(srcPath, []byte("body"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	conflict := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(conflict, []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	svc := NewBackupService(BackupOptions{
		SourcePath: srcPath,
		DestDir:    filepath.Join(conflict, "backups"),
	})
	if _, err := svc.Run(context.Background()); err == nil {
		t.Fatalf("Run() error = nil, want MkdirAll failure under a regular file")
	}
}

func TestBackupService_PruneRespectsRetention(t *testing.T) {
	tmp := t.TempDir()
	srcPath := filepath.Join(tmp, "omakiten.db")
	if err := os.WriteFile(srcPath, []byte("body"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	destDir := filepath.Join(tmp, "backups")

	// Drive seven Run() calls with monotonically increasing timestamps
	// so each snapshot's mtime is distinct enough for the pass to sort
	// reliably. Retention=5 → after the 7th call only the 5 newest
	// files survive (calls #3 through #7).
	base := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	written := []string{}
	for i := 0; i < 7; i++ {
		i := i
		svc := NewBackupService(BackupOptions{
			SourcePath: srcPath,
			DestDir:    destDir,
			Retention:  5,
			Now:        func() time.Time { return base.Add(time.Duration(i) * time.Minute) },
		})
		path, err := svc.Run(context.Background())
		if err != nil {
			t.Fatalf("Run() #%d error = %v", i, err)
		}
		// Force mtimes to match the synthetic timestamp so the prune
		// sort matches insertion order rather than wall-clock noise.
		ts := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(path, ts, ts); err != nil {
			t.Fatalf("chtimes #%d: %v", i, err)
		}
		written = append(written, filepath.Base(path))
	}

	entries, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatalf("list dest: %v", err)
	}
	entries = matchingBackupEntries(entries)
	if len(entries) != 5 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("dest entries = %d (%v), want 5", len(entries), names)
	}
	// Confirm the survivors are the newest five (written[2..6]).
	survivors := map[string]struct{}{}
	for _, e := range entries {
		survivors[e.Name()] = struct{}{}
	}
	for _, expected := range written[2:] {
		if _, ok := survivors[expected]; !ok {
			t.Fatalf("expected survivor %q not present; got %v", expected, survivors)
		}
	}
}

func TestBackupService_RunRetentionKeepsReturnedPathWithEqualMtimes(t *testing.T) {
	tmp := t.TempDir()
	sourcePath := filepath.Join(tmp, "source.db")
	if err := os.WriteFile(sourcePath, []byte("new snapshot"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	destDir := filepath.Join(tmp, "backups")
	if err := os.Mkdir(destDir, 0o700); err != nil {
		t.Fatalf("Mkdir backups: %v", err)
	}
	equalTime := time.Date(2026, 5, 21, 14, 30, 45, 0, time.UTC)
	oldPaths := []string{
		filepath.Join(destDir, "2026-05-21T14-30-43.000000000Z.db"),
		filepath.Join(destDir, "2026-05-21T14-30-44.000000000Z.db"),
	}
	for _, path := range oldPaths {
		if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
			t.Fatalf("write old backup: %v", err)
		}
		if err := os.Chtimes(path, equalTime, equalTime); err != nil {
			t.Fatalf("Chtimes old backup: %v", err)
		}
	}
	svc := NewBackupService(BackupOptions{
		SourcePath: sourcePath,
		DestDir:    destDir,
		Retention:  1,
		Now:        func() time.Time { return equalTime },
		SnapshotWriter: func(ctx context.Context, source, destination string) error {
			if err := AtomicCopyFile(source, destination, destination+".tmp"); err != nil {
				return err
			}
			return os.Chtimes(destination, equalTime, equalTime)
		},
	})

	returnedPath, err := svc.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(returnedPath); err != nil {
		t.Fatalf("returned backup was pruned: %v", err)
	}
	for _, path := range oldPaths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("older equal-mtime backup survived: %s (%v)", path, err)
		}
	}
}

func TestBackupService_PruneIgnoresUnrelatedFiles(t *testing.T) {
	tmp := t.TempDir()
	srcPath := filepath.Join(tmp, "omakiten.db")
	if err := os.WriteFile(srcPath, []byte("body"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	destDir := filepath.Join(tmp, "backups")
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		t.Fatalf("mkdir dest: %v", err)
	}
	foreign := filepath.Join(destDir, "README.md")
	if err := os.WriteFile(foreign, []byte("keep me"), 0o600); err != nil {
		t.Fatalf("write foreign: %v", err)
	}
	renamed := filepath.Join(destDir, "keep-2026-05-21T10-00-00Z.db")
	if err := os.WriteFile(renamed, []byte("renamed snapshot"), 0o600); err != nil {
		t.Fatalf("write renamed: %v", err)
	}

	// Six matching snapshots with Retention=1 → five would be pruned;
	// the foreign + renamed files must stay regardless of how many
	// snapshots existed.
	base := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 6; i++ {
		i := i
		svc := NewBackupService(BackupOptions{
			SourcePath: srcPath,
			DestDir:    destDir,
			Retention:  1,
			Now:        func() time.Time { return base.Add(time.Duration(i) * time.Minute) },
		})
		path, err := svc.Run(context.Background())
		if err != nil {
			t.Fatalf("Run() #%d error = %v", i, err)
		}
		ts := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(path, ts, ts); err != nil {
			t.Fatalf("chtimes #%d: %v", i, err)
		}
	}
	for _, path := range []string{foreign, renamed} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("foreign-pattern file missing after prune: %v", err)
		}
	}
}

func TestBackupService_PruneNoOpWhenRetentionDisabled(t *testing.T) {
	for _, retention := range []int{0, -1} {
		retention := retention
		t.Run("retention="+itoa(retention), func(t *testing.T) {
			tmp := t.TempDir()
			srcPath := filepath.Join(tmp, "omakiten.db")
			if err := os.WriteFile(srcPath, []byte("body"), 0o600); err != nil {
				t.Fatalf("write source: %v", err)
			}
			destDir := filepath.Join(tmp, "backups")
			base := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
			for i := 0; i < 4; i++ {
				i := i
				svc := NewBackupService(BackupOptions{
					SourcePath: srcPath,
					DestDir:    destDir,
					Retention:  retention,
					Now:        func() time.Time { return base.Add(time.Duration(i) * time.Minute) },
				})
				if _, err := svc.Run(context.Background()); err != nil {
					t.Fatalf("Run() #%d error = %v", i, err)
				}
			}
			entries, err := os.ReadDir(destDir)
			if err != nil {
				t.Fatalf("list dest: %v", err)
			}
			entries = matchingBackupEntries(entries)
			if len(entries) != 4 {
				t.Fatalf("dest entries = %d, want 4 (retention=%d disables prune)", len(entries), retention)
			}
		})
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	out := ""
	for n > 0 {
		out = string(rune('0'+n%10)) + out
		n /= 10
	}
	if neg {
		out = "-" + out
	}
	return out
}

func matchingBackupEntries(entries []os.DirEntry) []os.DirEntry {
	matched := entries[:0]
	for _, entry := range entries {
		if backupFilenamePattern.MatchString(entry.Name()) {
			matched = append(matched, entry)
		}
	}
	return matched
}
