package cli

import (
	"bufio"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/paths"
)

func newProjectsCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "projects",
		Short: opts.t("cli.projects.short"),
	}
	cmd.AddCommand(newProjectsDeleteCommand(opts))
	return cmd
}

func newProjectsDeleteCommand(opts *runtimeOptions) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "delete <id-or-slug>",
		Short: opts.t("cli.projects.delete.short"),
		Long:  opts.t("cli.projects.delete.long"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				return runProjectsDelete(ctx, cmd, opts, args[0], yes)
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, opts.t("cli.projects.delete.flag.yes"))
	return cmd
}

func runProjectsDelete(ctx context.Context, cmd *cobra.Command, opts *runtimeOptions, target string, yes bool) (any, error) {
	rt, err := opts.open(ctx, true)
	if err != nil {
		return nil, err
	}
	defer rt.close()
	ctx = rt.WithActivityRepo(ctx)

	project, err := resolveProjectTarget(ctx, rt.store, target)
	if err != nil {
		return nil, err
	}

	counters, err := rt.store.ProjectDeleteCounts(ctx, project.ID)
	if err != nil {
		return nil, err
	}

	if !yes {
		if !stdinIsTTY() {
			return nil, domain.NewError(domain.ErrValidation, opts.t("cli.projects.delete.err.no_tty"), nil)
		}
		if err := promptDeleteConfirmation(cmd, opts, project, counters); err != nil {
			return nil, err
		}
	}

	backup := buildBackupService(rt.dbPath, cmd, opts)
	svc := app.NewProjectService(rt.store, backup, rt.store)
	result, err := svc.Delete(ctx, project.ID)
	if err != nil {
		return nil, err
	}

	fmt.Fprintf(cmd.ErrOrStderr(), opts.t("cli.projects.delete.success_fmt")+"\n", result.Project.Slug, result.BackupPath)
	return map[string]any{
		"project":     result.Project,
		"counters":    result.Counters,
		"backup_path": result.BackupPath,
		"event_type":  result.EventType,
	}, nil
}

// resolveProjectTarget accepts either a numeric id or a slug. Numeric
// inputs route through FindProjectByID so an integer slug like "1234"
// can be disambiguated by quoting (`okt projects delete "1234"` still
// resolves as a slug if the lookup-by-id fails). The fall-through
// behaviour matches `okt --project` resolution.
func resolveProjectTarget(ctx context.Context, store app.ProjectRepository, target string) (domain.Project, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return domain.Project{}, domain.NewError(domain.ErrValidation, "target is required", nil)
	}
	if id, err := strconv.ParseInt(target, 10, 64); err == nil {
		if project, err := store.FindProjectByID(ctx, id); err == nil {
			return project, nil
		}
	}
	return store.FindProjectBySlug(ctx, target)
}

// promptDeleteConfirmation renders the counters summary, the project
// name, and reads a single line from stdin. Any response other than
// `y` or `Y` aborts with a validation_error so the JSON envelope
// carries a coded failure instead of a silent exit.
func promptDeleteConfirmation(cmd *cobra.Command, opts *runtimeOptions, project domain.Project, counters domain.ProjectDeleteCounters) error {
	fmt.Fprintf(cmd.ErrOrStderr(), opts.t("cli.projects.delete.counters_fmt")+"\n",
		counters.Tasks, counters.Comments, counters.Plans, counters.Tags, counters.ActivityLogEntries)
	fmt.Fprintf(cmd.ErrOrStderr(), opts.t("cli.projects.delete.prompt")+" ", project.Name)
	reader := bufio.NewReader(cmd.InOrStdin())
	line, err := reader.ReadString('\n')
	if err != nil {
		return domain.NewError(domain.ErrValidation, opts.t("cli.projects.delete.err.aborted"), nil)
	}
	answer := strings.TrimSpace(strings.ToLower(line))
	if answer != "y" {
		return domain.NewError(domain.ErrValidation, opts.t("cli.projects.delete.err.aborted"), nil)
	}
	return nil
}

// buildBackupService wires the same BackupService the standalone
// `okt db backup` command uses. Sharing one constructor keeps the
// snapshot directory + retention contract identical across every
// destructive flow.
func buildBackupService(dbPath string, cmd *cobra.Command, opts *runtimeOptions) *app.BackupService {
	destDir, err := paths.BackupDir()
	if err != nil {
		destDir = ""
	}
	retention := 0
	if configPath, err := opts.resolvedConfigPath(); err == nil {
		if bundle, err := config.LoadBundle(configPath); err == nil {
			retention = bundle.Config.Backup.RetentionCount
		}
	}
	return app.NewBackupService(app.BackupOptions{
		SourcePath:      dbPath,
		DestDir:         destDir,
		Retention:       retention,
		Stderr:          cmd.ErrOrStderr(),
		PruneWarnFormat: opts.t("cli.db.backup.prune_warn_fmt"),
	})
}
