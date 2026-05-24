package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"omakiten/internal/app"
)

// forbiddenBackupOutRoots are the absolute path prefixes the `db backup
// --out` flag refuses to write into. Catches the common slip of typing
// "/etc/foo.db" or similar — the snapshot carries every project's data
// and dropping it into a system tree (a) leaks across users on a shared
// box and (b) almost certainly is not what the operator intended.
// Resolved relative to filepath.Separator at check time so the same
// constant works on either path style. Order does not matter — the
// guard exits on the first match.
var forbiddenBackupOutRoots = []string{"/etc", "/usr", "/proc", "/sys", "/dev"}

func newDBCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: opts.t("cli.db.short"),
	}
	cmd.AddCommand(newDBBackupCommand(opts))
	return cmd
}

func newDBBackupCommand(opts *runtimeOptions) *cobra.Command {
	var out string
	var force bool

	cmd := &cobra.Command{
		Use:   "backup",
		Short: opts.t("cli.db.backup.short"),
		Long:  opts.t("cli.db.backup.long"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				return runDBBackup(ctx, cmd, opts, out, force)
			})
		},
	}
	cmd.Flags().StringVarP(&out, "out", "o", "", opts.t("cli.db.backup.flag.out"))
	cmd.Flags().BoolVar(&force, "force", false, opts.t("cli.db.backup.flag.force"))
	return cmd
}

// runDBBackup wires the BackupService against the resolved DB and
// config paths. The store is intentionally left unopened — the snapshot
// is a plain file copy and opening a sibling SQLite handle here would
// race with a concurrent TUI write. The bundle is loaded via
// buildCLIBackupService so the retention knob threads through without a
// runtime/cache spin-up; the standalone command runs in soft-strict
// mode so a partially-migrated config does not block recovery.
//
// When --out is supplied the snapshot is written to that exact path
// (parent dirs created as needed with 0o700 to match the default
// BackupDir perm — DB snapshots carry every project's data and must
// not relax permissions because the user picked a path) and the prune
// pass is skipped — the user pinned the destination, so retention
// rotation against the default state directory does not apply.
func runDBBackup(ctx context.Context, cmd *cobra.Command, opts *runtimeOptions, out string, force bool) (any, error) {
	dbPath, err := opts.resolvedDBPath()
	if err != nil {
		return nil, err
	}

	if out != "" {
		finalPath, err := filepath.Abs(out)
		if err != nil {
			return nil, err
		}
		finalPath = filepath.Clean(finalPath)
		if root, blocked := blockedBackupOutRoot(finalPath); blocked {
			return nil, fmt.Errorf(opts.t("cli.db.backup.error.system_path_fmt"), finalPath, root)
		}
		if !force {
			if _, statErr := os.Stat(finalPath); statErr == nil {
				return nil, fmt.Errorf(opts.t("cli.db.backup.error.exists_fmt"), finalPath)
			} else if !errors.Is(statErr, os.ErrNotExist) {
				return nil, fmt.Errorf("backup --out stat: %w", statErr)
			}
		}
		if err := os.MkdirAll(filepath.Dir(finalPath), 0o700); err != nil {
			return nil, fmt.Errorf("backup --out parent: %w", err)
		}
		if _, err := os.Stat(dbPath); err != nil {
			return nil, fmt.Errorf("backup source: %w", err)
		}
		tmp := finalPath + ".tmp"
		if err := app.AtomicCopyFile(dbPath, finalPath, tmp); err != nil {
			return nil, err
		}
		fmt.Fprintf(cmd.ErrOrStderr(), opts.t("cli.db.backup.success_fmt")+"\n", finalPath)
		return map[string]any{"path": finalPath, "pruned": false}, nil
	}

	svc, retention, err := buildCLIBackupService(cmd, opts, dbPath, false)
	if err != nil {
		return nil, err
	}
	finalPath, err := svc.Run(ctx)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), opts.t("cli.db.backup.success_fmt")+"\n", finalPath)
	return map[string]any{"path": finalPath, "pruned": true, "retention": retention}, nil
}

// blockedBackupOutRoot reports whether the cleaned absolute path lives
// inside one of the forbiddenBackupOutRoots. Returns the matched root
// so the caller's error message can name the rule that fired. Strict
// prefix check: "/etc/foo" matches "/etc", but "/etcetera" does not.
func blockedBackupOutRoot(absClean string) (string, bool) {
	for _, root := range forbiddenBackupOutRoots {
		if absClean == root || strings.HasPrefix(absClean, root+string(filepath.Separator)) {
			return root, true
		}
	}
	return "", false
}
