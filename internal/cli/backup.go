package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/paths"
	"omakiten/internal/sqlite"
)

// buildCLIBackupService is the single composition-root for every CLI
// flow that snapshots the live DB. The standalone `okt db backup`
// command, `okt projects delete`, and the `okt update` pre-swap hook
// all funnel through here so the snapshot directory, retention contract,
// and prune-warning surface stay aligned across callers.
//
// strict=true (destructive flows) demands a resolvable BackupDir +
// loadable bundle and surfaces every miss as an error so the
// "auto-backup is non-optional" invariant (#191 AC #36, #39) is not
// silently bypassed. strict=false (standalone backup) keeps the soft
// fallback the spec calls for — a partially-migrated config should not
// block recovery work — and emits a stderr warning naming the cause so
// the user still sees that retention defaulted to zero.
func buildCLIBackupService(cmd *cobra.Command, opts *runtimeOptions, dbPath string, strict bool) (*app.BackupService, int, error) {
	destDir, err := paths.BackupDir()
	if err != nil {
		return nil, 0, fmt.Errorf("resolve backup dir: %w", err)
	}

	retention, rerr := resolveBackupRetention(opts)
	if rerr != nil {
		if strict {
			return nil, 0, fmt.Errorf("resolve backup retention: %w", rerr)
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: backup retention defaulted to 0 (%s)\n", rerr.Error())
	}

	stderr := cmd.ErrOrStderr()
	warnFormat := opts.t("cli.db.backup.prune_warn_fmt")
	svc := app.NewBackupService(app.BackupOptions{
		SourcePath:     dbPath,
		DestDir:        destDir,
		Retention:      retention,
		SnapshotWriter: sqlite.SnapshotDatabase,
		PruneWarn: func(pruneErr error) {
			fmt.Fprintf(stderr, warnFormat+"\n", pruneErr.Error())
		},
	})
	return svc, retention, nil
}

// resolveBackupRetention reads settings.backup.retention_count from the
// active config bundle. Returns (0, err) when the bundle cannot be
// loaded — destructive callers escalate via strict mode; the standalone
// backup command degrades gracefully with a stderr warning.
func resolveBackupRetention(opts *runtimeOptions) (int, error) {
	configPath, err := opts.resolvedConfigPath()
	if err != nil {
		return 0, fmt.Errorf("resolve config path: %w", err)
	}
	bundle, err := config.LoadBundle(configPath)
	if err != nil {
		return 0, fmt.Errorf("load bundle %s: %w", configPath, err)
	}
	return bundle.Config.Backup.RetentionCount, nil
}
