package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

// BackupService writes a rolling snapshot of the SQLite database file
// to a configured directory and auto-prunes older snapshots so the
// directory stays bounded. Same routine is invoked by the standalone
// `okt db backup` CLI command, and (in later PRs) by every destructive
// command that runs an auto-backup before mutating state — keeping one
// implementation prevents the prune contract from drifting per caller.
//
// The copy is intentionally a plain file read+write+rename rather than
// a sqlite-level VACUUM INTO. The simple shape mirrors AC #3 of task
// #191 and avoids opening a sibling connection that would race with a
// concurrently running TUI; a true online snapshot is deferred to a
// follow-up if the WAL drift becomes observable in practice.
type BackupService struct {
	sourcePath  string
	destDir     string
	retention   int
	now         func() time.Time
	stderr      io.Writer
	pruneFormat string
}

// BackupOptions bundles every input NewBackupService consumes so the
// constructor stays one positional argument and callers (CLI bootstrap,
// future ProjectService.Delete) can leave optional knobs at their
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
	// Stderr is where prune warnings are written. nil falls back to
	// os.Stderr — production wires the cobra command's ErrOrStderr.
	Stderr io.Writer
	// PruneWarnFormat is the Printf-style format Run uses when a
	// prune pass fails after a successful backup. Carries exactly one
	// `%s` substitution (the underlying error). Empty falls back to
	// the literal "prune skipped: %s" — production wires the
	// localized `cli.db.backup.prune_warn_fmt` catalog entry so the
	// stderr line follows the user's CLI language.
	PruneWarnFormat string
}

// NewBackupService returns a BackupService ready to Run. The returned
// pointer is safe to call from a single goroutine per invocation;
// concurrent Run calls are not synchronised because every snapshot
// uses a fresh, distinct destination filename (utc-iso second
// granularity) and the prune pass operates on a directory listing
// taken at call time.
func NewBackupService(opts BackupOptions) *BackupService {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	pruneFormat := opts.PruneWarnFormat
	if pruneFormat == "" {
		pruneFormat = "prune skipped: %s"
	}
	return &BackupService{
		sourcePath:  opts.SourcePath,
		destDir:     opts.DestDir,
		retention:   opts.Retention,
		now:         now,
		stderr:      stderr,
		pruneFormat: pruneFormat,
	}
}

// backupFilenamePattern matches the snapshot basename written by Run.
// Strict on purpose: pruneBackups uses it as a gate so foreign files a
// user drops in the backup dir (a manual `cp prod.db .`, a Time
// Machine artifact, anything not matching this exact pattern) survive
// every retention pass. The trailing `.db` is required so renaming a
// snapshot to mark it "keep" reliably opts it out of prune.
var backupFilenamePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}Z\.db$`)

// Run copies sourcePath into destDir as a new snapshot and triggers a
// prune pass. The destination filename is `<utc-iso>.db` with the ISO
// 8601 second component using `-` separators (cross-platform: Windows
// rejects `:` in filenames). On a copy failure mid-write the temporary
// file is removed so the destination never contains a partial
// snapshot. Prune failures are surfaced as stderr warnings and do not
// fail Run (AC #44) — the snapshot itself is already on disk and the
// caller's "backup → <path>" status remains accurate.
func (s *BackupService) Run(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if _, err := os.Stat(s.sourcePath); err != nil {
		return "", fmt.Errorf("backup source: %w", err)
	}
	if err := os.MkdirAll(s.destDir, 0o700); err != nil {
		return "", fmt.Errorf("backup dest dir: %w", err)
	}

	stamp := s.now().UTC().Format("2006-01-02T15-04-05Z")
	finalPath := filepath.Join(s.destDir, stamp+".db")
	tmpPath := finalPath + ".tmp"

	if err := AtomicCopyFile(s.sourcePath, finalPath, tmpPath); err != nil {
		return "", err
	}

	if err := s.pruneBackups(); err != nil {
		fmt.Fprintf(s.stderr, s.pruneFormat+"\n", err.Error())
	}
	return finalPath, nil
}

// AtomicCopyFile streams src into a temporary file at tmpPath and then
// renames it to dstPath. Callers pin the tmp filename so they can keep
// the rename target on the same filesystem as dstPath (os.Rename is
// only atomic on a single filesystem). On any failure the tmp is
// removed so the caller never observes a half-written destination.
// Shared by BackupService and the `okt db backup --out` flow so the
// rolling-snapshot path and the user-pinned path enforce identical
// atomicity guarantees.
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

// pruneBackups deletes snapshots beyond the retention cap. Files whose
// basename does not match backupFilenamePattern are left alone so the
// pass cannot collateral-damage user-renamed snapshots, README files
// the user drops in the directory, or filesystem artifacts. mtime sort
// (descending) reflects when a snapshot was written rather than parsing
// the timestamp from the basename — keeps the pass robust to clock
// skew between machines that share the directory over a sync tool.
func (s *BackupService) pruneBackups() error {
	if s.retention <= 0 {
		return nil
	}
	entries, err := os.ReadDir(s.destDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("list backup dir: %w", err)
	}

	type backupEntry struct {
		path  string
		mtime time.Time
	}
	candidates := make([]backupEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !backupFilenamePattern.MatchString(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		candidates = append(candidates, backupEntry{
			path:  filepath.Join(s.destDir, entry.Name()),
			mtime: info.ModTime(),
		})
	}
	if len(candidates) <= s.retention {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].mtime.After(candidates[j].mtime)
	})
	for _, expired := range candidates[s.retention:] {
		if err := os.Remove(expired.path); err != nil {
			return fmt.Errorf("remove %s: %w", expired.path, err)
		}
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
