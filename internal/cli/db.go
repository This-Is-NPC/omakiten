package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"omakiten/internal/agentruntime"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/operation"
	"omakiten/internal/sqlite"
)

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
				return runDBReindex(ctx, cmd, opts, confirm)
			})
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, opts.t("cli.db.reindex.flag.confirm"))
	return cmd
}

func runDBReindex(ctx context.Context, cmd *cobra.Command, opts *runtimeOptions, confirm bool) (any, error) {
	dbPath, err := opts.resolvedDBPath()
	if err != nil {
		return nil, err
	}
	maintenance := agentruntime.MaintenanceOptions{
		DBPath: dbPath,
		OpenSearchStore: func(ctx context.Context) (*sqlite.Store, error) {
			return openExistingSearchStoreAt(ctx, opts, dbPath)
		},
	}
	// The unconfirmed plan writes no backup, so it loads no bundle for retention.
	svc := operation.NewService(nil, contract.ProjectSelector{}).ForCLI()
	if confirm {
		if err := bindMaintenance(cmd, opts, svc, maintenance, false); err != nil {
			return nil, err
		}
	} else {
		maintenance.Catalog = opts.catalog
		svc.SetMaintenance(agentruntime.NewMaintenance(maintenance))
	}
	result, err := svc.ReindexSearch(ctx, contract.SearchReindexInput{Confirm: confirm})
	if err != nil {
		if !confirm {
			return nil, reindexConfirmationErrorWithRetryGuidance(err, dbPath, opts.t("cli.db.reindex.error.confirm_required"))
		}
		return nil, err
	}
	printPruneWarnings(cmd, opts, result.PruneWarnings)
	if result.LeaseReleaseWarning != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: backup lease release failed after reindex committed (%s)\n", result.LeaseReleaseWarning)
	}
	if result.BackupRecommended && result.BackupPath != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), opts.t("cli.db.reindex.warning.backup")+"\n", result.BackupPath)
	}
	return result, nil
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

// runDBBackup snapshots the database through the operation facade. SQLite's
// online snapshot includes committed WAL frames without checkpointing or
// mutating the source. --out pins the destination and skips retention
// pruning; the default destination is the retained backup directory.
func runDBBackup(ctx context.Context, cmd *cobra.Command, opts *runtimeOptions, out string, force bool) (any, error) {
	dbPath, err := opts.resolvedDBPath()
	if err != nil {
		return nil, err
	}
	svc, err := databaseMaintenanceService(cmd, opts, agentruntime.MaintenanceOptions{DBPath: dbPath}, false)
	if err != nil {
		return nil, err
	}
	result, err := svc.BackupDatabase(ctx, contract.DatabaseBackupInput{Out: out, Force: force})
	if err != nil {
		return nil, err
	}
	printPruneWarnings(cmd, opts, result.PruneWarnings)
	fmt.Fprintf(cmd.ErrOrStderr(), opts.t("cli.db.backup.success_fmt")+"\n", result.Path)
	return result, nil
}
