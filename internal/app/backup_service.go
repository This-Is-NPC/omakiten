package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// SnapshotWriter writes one complete source snapshot to its final destination.
type SnapshotWriter func(context.Context, string, string) error

// BackupService writes a rolling database snapshot to a configured directory
// and auto-prunes older snapshots so the
// directory stays bounded. Same routine is invoked by the standalone
// `okt db backup` CLI command, and by every destructive command that
// runs an auto-backup before mutating state — keeping one
// implementation prevents the prune contract from drifting per caller.
type BackupService struct {
	sourcePath     string
	destDir        string
	retention      int
	now            func() time.Time
	pruneWarn      func(error)
	snapshotWriter SnapshotWriter
}

// BackupOptions bundles every input NewBackupService consumes so the
// constructor stays one positional argument and callers (CLI bootstrap,
// ProjectService.Delete) can leave optional knobs at their
// zero-value defaults.
type BackupOptions struct {
	// SourcePath is the absolute path of the live SQLite database file.
	// Required. Run returns an error when it does not exist.
	SourcePath string
	// DestDir is the directory snapshots are written into. Created with
	// mode 0700 when missing. Required.
	DestDir string
	// Retention is the count of snapshots pruneBackups keeps after each
	// successful Run. Values <= 0 disable prune (snapshots accumulate).
	Retention int
	// Now is the clock used for the snapshot filename. nil falls back to
	// time.Now — tests pin a deterministic value.
	Now func() time.Time
	// SnapshotWriter performs the context-aware snapshot write. nil preserves
	// the generic atomic file-copy behavior for non-SQLite callers and tests.
	SnapshotWriter SnapshotWriter
	// PruneWarn is invoked when a prune pass fails after a successful
	// snapshot write. The snapshot itself is already on disk, so prune
	// failures never abort Run — callers route the error to the
	// surface that matches their UX (CLI: localized stderr line; TUI:
	// status badge; tests: capture). nil disables the notification —
	// the prune error is silently dropped. Keeping i18n on the caller
	// side removes the format-string mismatch risk that a Printf
	// template inside the service would carry.
	PruneWarn func(error)
}

// NewBackupService returns a BackupService ready to Run. Run calls across
// independent service instances and processes serialize through the backup
// directory lease.
func NewBackupService(opts BackupOptions) *BackupService {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	snapshotWriter := opts.SnapshotWriter
	if snapshotWriter == nil {
		snapshotWriter = func(ctx context.Context, sourcePath, destinationPath string) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(destinationPath), 0o700); err != nil {
				return fmt.Errorf("backup dest dir: %w", err)
			}
			return AtomicCopyFile(sourcePath, destinationPath, destinationPath+".tmp")
		}
	}
	return &BackupService{
		sourcePath:     opts.SourcePath,
		destDir:        opts.DestDir,
		retention:      opts.Retention,
		now:            now,
		pruneWarn:      opts.PruneWarn,
		snapshotWriter: snapshotWriter,
	}
}

// backupFilenamePattern matches the snapshot basename written by Run.
// Strict on purpose: pruneBackups uses it as a gate so foreign files a
// user drops in the backup dir (a manual `cp prod.db .`, a Time
// Machine artifact, anything not matching this exact pattern) survive
// every retention pass. The trailing `.db` is required so renaming a
// snapshot to mark it "keep" reliably opts it out of prune. The
// nanosecond suffix is optional so snapshots written by prior
// (second-granularity) versions still match.
var backupFilenamePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}(\.\d{9})?Z\.db$`)

// Run writes sourcePath into destDir through the configured snapshot writer and triggers a
// prune pass. The destination filename is `<utc-iso>.db` with the ISO
// 8601 second + nanosecond components using `-` separators (cross-platform:
// Windows rejects `:` in filenames). Nanosecond granularity prevents
// rapid back-to-back invocations (e.g. delete + update in the same
// second) from colliding on the destination name and silently
// overwriting the earlier snapshot. Snapshot writers must remove temporary
// files on failure so the destination never contains a partial snapshot.
// Prune failures are routed to the configured PruneWarn
// callback and do not fail Run — the snapshot itself is already on
// disk and the caller's "backup → <path>" status remains accurate.
func (s *BackupService) Run(ctx context.Context) (string, error) {
	var finalPath string
	err := s.WithLease(ctx, func(lease BackupLease) error {
		var err error
		finalPath, err = lease.Write(ctx)
		if err != nil {
			return err
		}
		_ = lease.PruneRetaining(finalPath)
		return nil
	})
	return finalPath, err
}

func (s *BackupService) write(ctx context.Context, write func(string) error) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if _, err := os.Stat(s.sourcePath); err != nil {
		return "", fmt.Errorf("backup source: %w", err)
	}
	stamp := s.now().UTC().Format("2006-01-02T15-04-05.000000000Z")
	finalPath := filepath.Join(s.destDir, stamp+".db")
	if write != nil {
		if err := write(finalPath); err != nil {
			return "", err
		}
		return finalPath, nil
	}
	if err := s.snapshotWriter(ctx, s.sourcePath, finalPath); err != nil {
		return "", err
	}
	return finalPath, nil
}

// AtomicCopyFile streams src into a temporary file at tmpPath and then
// renames it to dstPath. Callers pin the tmp filename so they can keep
// the rename target on the same filesystem as dstPath (os.Rename is
// only atomic on a single filesystem). On any failure the tmp is
// removed so the caller never observes a half-written destination.
// This remains BackupService's default writer for generic callers. SQLite CLI
// composition injects its online snapshot writer instead.
func AtomicCopyFile(src, dstPath, tmpPath string) error {
	if err := copyFile(src, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename %s -> %s: %w", tmpPath, dstPath, err)
	}
	return nil
}

// copyFile streams src into dst with a defer-close pair so a write
// failure between create and copy still releases the file handle. Mode
// 0600 matches the live DB's permissions — the snapshot directory is
// already 0700 so this is belt-and-suspenders against an inheriting
// umask.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open dest: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("copy: %w", err)
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return fmt.Errorf("sync: %w", err)
	}
	return out.Close()
}
