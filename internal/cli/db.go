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
	"omakiten/internal/domain"
	"omakiten/internal/sqlite"
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
	cmd.AddCommand(newDBCheckCommand(opts))
	cmd.AddCommand(newDBReindexCommand(opts))
	return cmd
}

func newDBCheckCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: opts.t("cli.db.check.short"),
		Long:  opts.t("cli.db.check.long"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				store, err := openExistingSearchStore(ctx, opts)
				if err != nil {
					return nil, err
				}
				defer func() { _ = store.Close() }()
				report, err := store.CheckSearchIndex(ctx)
				if err != nil {
					return nil, err
				}
				if !report.Healthy {
					return nil, domain.NewError(domain.ErrSearchIndexInvalid, opts.t("cli.db.check.error.invalid"), map[string]any{
						"report": report,
					})
				}
				return report, nil
			})
		},
	}
}

func newDBReindexCommand(opts *runtimeOptions) *cobra.Command {
	var confirm bool
	cmd := &cobra.Command{
		Use:   "reindex",
		Short: opts.t("cli.db.reindex.short"),
		Long:  opts.t("cli.db.reindex.long"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				dbPath, err := opts.resolvedDBPath()
				if err != nil {
					return nil, err
				}
				store, err := openExistingSearchStoreAt(ctx, opts, dbPath)
				if err != nil {
					return nil, err
				}
				defer func() { _ = store.Close() }()
				backupPath := ""
				var result domain.SearchIndexReindexReport
				if confirm {
					backup, _, err := buildCLIBackupService(cmd, opts, dbPath, false)
					if err != nil {
						return nil, err
					}
					operation, leaseErr := app.RunLeasedDestructiveOperation(ctx, backup, func(lease app.RecoveryLease) app.DestructiveOperationResult {
						createBackup := func(backupCtx context.Context, write func(string) error) (string, error) {
							return lease.WriteSnapshot(backupCtx, write)
						}
						var operationErr error
						result, backupPath, operationErr = store.ReindexSearchConfirmedWithBackup(ctx, createBackup, lease.Discard, lease.Validate)
						return app.DestructiveOperationResult{
							BackupPath:        backupPath,
							MutationCompleted: operationErr == nil,
							Err:               operationErr,
						}
					})
					backupPath = operation.BackupPath
					if !operation.MutationCompleted {
						if operation.Err != nil {
							return nil, fmt.Errorf("verified backup and search reindex: %w", errors.Join(operation.Err, leaseErr))
						}
						return nil, fmt.Errorf("acquire reindex backup lease: %w", leaseErr)
					}
					if leaseErr != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "warning: backup lease release failed after reindex committed (%s)\n", leaseErr.Error())
					}
				} else {
					result, err = store.ReindexSearchConfirmed(ctx, false)
					if err != nil {
						return nil, reindexConfirmationErrorWithRetryGuidance(err, dbPath, opts.t("cli.db.reindex.error.confirm_required"))
					}
				}
				if result.BackupRecommended && backupPath != "" {
					fmt.Fprintf(cmd.ErrOrStderr(), opts.t("cli.db.reindex.warning.backup")+"\n", backupPath)
				}
				return dbReindexResponse{
					SearchIndexReindexReport: result,
					DatabasePath:             dbPath,
					BackupPath:               backupPath,
				}, nil
			})
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, opts.t("cli.db.reindex.flag.confirm"))
	return cmd
}

type dbReindexResponse struct {
	domain.SearchIndexReindexReport
	DatabasePath string `json:"database_path"`
	BackupPath   string `json:"backup_path,omitempty"`
}

func dbReindexRetryGuidance(dbPath string) (string, []string) {
	args := []string{"--db", dbPath, "db", "reindex", "--confirm"}
	return "okt --db " + shellQuoteArg(dbPath) + " db reindex --confirm", args
}

func reindexConfirmationErrorWithRetryGuidance(err error, dbPath, messageFormat string) error {
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrValidation || coded.Details["requires_confirmation"] != true {
		return err
	}
	command, args := dbReindexRetryGuidance(dbPath)
	details := make(map[string]any, len(coded.Details)+3)
	for key, value := range coded.Details {
		details[key] = value
	}
	details["database_path"] = dbPath
	details["retry_command"] = command
	details["retry_args"] = args
	return domain.NewError(coded.Code, fmt.Sprintf(messageFormat, command), details)
}

// openExistingSearchStore is deliberately separate from runtimeOptions.open:
// db maintenance must not load or validate a config bundle, and must stat the
// source before sqlite.Open can create it as a side effect.
func openExistingSearchStore(ctx context.Context, opts *runtimeOptions) (*sqlite.Store, error) {
	dbPath, err := opts.resolvedDBPath()
	if err != nil {
		return nil, err
	}
	return openExistingSearchStoreAt(ctx, opts, dbPath)
}

func openExistingSearchStoreAt(ctx context.Context, opts *runtimeOptions, dbPath string) (*sqlite.Store, error) {
	info, err := os.Lstat(dbPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, domain.NewError(domain.ErrValidation, fmt.Sprintf(opts.t("cli.db.error.missing_fmt"), dbPath), map[string]any{
				"path": dbPath,
			})
		}
		return nil, fmt.Errorf("database stat: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, domain.NewError(domain.ErrValidation, fmt.Sprintf(opts.t("cli.db.error.not_file_fmt"), dbPath), map[string]any{
			"path": dbPath,
		})
	}
	return sqlite.OpenSearchMaintenance(ctx, dbPath)
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

// runDBBackup wires the BackupService against the resolved DB and config
// paths. SQLite's online snapshot mechanism includes committed WAL frames
// without checkpointing or mutating the source. The bundle is loaded via
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
			return nil, domain.NewError(
				domain.ErrValidation,
				fmt.Sprintf(opts.t("cli.db.backup.error.system_path_fmt"), finalPath, root),
				map[string]any{"path": finalPath, "root": root},
			)
		}
		if !force {
			if _, statErr := os.Stat(finalPath); statErr == nil {
				return nil, domain.NewError(
					domain.ErrValidation,
					fmt.Sprintf(opts.t("cli.db.backup.error.exists_fmt"), finalPath),
					map[string]any{"path": finalPath},
				)
			} else if !errors.Is(statErr, os.ErrNotExist) {
				return nil, fmt.Errorf("backup --out stat: %w", statErr)
			}
		}
		snapshot := sqlite.SnapshotDatabase
		if force {
			snapshot = sqlite.SnapshotDatabaseReplace
		}
		if err := snapshot(ctx, dbPath, finalPath); err != nil {
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
