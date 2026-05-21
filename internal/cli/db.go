package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"omakiten/internal/app"
)

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

	cmd := &cobra.Command{
		Use:   "backup",
		Short: opts.t("cli.db.backup.short"),
		Long:  opts.t("cli.db.backup.long"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				return runDBBackup(ctx, cmd, opts, out)
			})
		},
	}
	cmd.Flags().StringVarP(&out, "out", "o", "", opts.t("cli.db.backup.flag.out"))
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
func runDBBackup(ctx context.Context, cmd *cobra.Command, opts *runtimeOptions, out string) (any, error) {
	dbPath, err := opts.resolvedDBPath()
	if err != nil {
		return nil, err
	}

	if out != "" {
		finalPath, err := filepath.Abs(out)
		if err != nil {
			return nil, err
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
