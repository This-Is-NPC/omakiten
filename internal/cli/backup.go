package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"omakiten/internal/agentruntime"
	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/operation"
)

// bindMaintenance is the single composition root for every CLI flow that
// snapshots the live DB. `okt db backup`, `okt db reindex`,
// `okt projects delete`, and the `okt update` pre-swap hook all funnel
// through here so the snapshot directory, retention contract, and
// prune-warning surface stay aligned across callers.
//
// strict=true (destructive flows) demands a loadable bundle and surfaces a
// miss as an error so the auto-backup cannot be silently bypassed.
// strict=false (standalone backup) keeps the soft fallback — a partially
// migrated config must not block recovery work — and warns on stderr that
// retention defaulted to zero.
func bindMaintenance(cmd *cobra.Command, opts *runtimeOptions, svc *operation.Service, maintenance agentruntime.MaintenanceOptions, strict bool) error {
	retention, err := resolveBackupRetention(opts)
	if err != nil {
		if strict {
			return fmt.Errorf("resolve backup retention: %w", err)
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: backup retention defaulted to 0 (%s)\n", err.Error())
	}
	maintenance.Retention = retention
	maintenance.Catalog = opts.catalog
	svc.SetMaintenance(agentruntime.NewMaintenance(maintenance))
	return nil
}

// databaseMaintenanceService binds maintenance to a CLI service that loads
// no workflow: database recovery must run while the configuration is broken.
func databaseMaintenanceService(cmd *cobra.Command, opts *runtimeOptions, maintenance agentruntime.MaintenanceOptions, strict bool) (*operation.Service, error) {
	svc := operation.NewService(nil, contract.ProjectSelector{}).ForCLI()
	if err := bindMaintenance(cmd, opts, svc, maintenance, strict); err != nil {
		return nil, err
	}
	return svc, nil
}

func printPruneWarnings(cmd *cobra.Command, opts *runtimeOptions, warnings []string) {
	for _, warning := range warnings {
		fmt.Fprintf(cmd.ErrOrStderr(), opts.t("cli.db.backup.prune_warn_fmt")+"\n", warning)
	}
}

// resolveBackupRetention reads settings.backup.retention_count from the
// active config bundle. Returns (0, err) when the bundle cannot be loaded.
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
