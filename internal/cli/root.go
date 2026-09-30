package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"omakiten/internal/activity"
	"omakiten/internal/agentruntime"
	"omakiten/internal/config"
	"omakiten/internal/configstore"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/events"
	"omakiten/internal/hooks"
	"omakiten/internal/hooks/actions"
	"omakiten/internal/operation"
	"omakiten/internal/output"
	"omakiten/internal/paths"
	projectresolver "omakiten/internal/project"
	"omakiten/internal/sqlite"
)

type runtimeOptions struct {
	dbPath     string
	configPath string
	project    string
	projectID  int64
	// discoveryStart is the directory FindRepoLocal walks up from. open()
	// populates it after resolving --project (project.root_path) or falls
	// back to CWD. Reset between calls to open(); resolver helpers below
	// honour it.
	discoveryStart string
	// catalog is the i18n catalog the CLI uses to resolve every Short,
	// Long, flag usage and user-facing error string. Built once by
	// NewRootCommand via bootstrapCatalog and shared across every
	// subcommand constructor — Catalog.Get is safe on a nil receiver
	// (returns the key literal) so subcommands never need to guard.
	catalog *config.Catalog
}

// t returns the localized string for key. Centralizes the catalog read
// at the CLI boundary so command constructors stay one short call per
// literal: `Short: opts.t("cli.task.add.short")` instead of inlining
// nil checks and namespace fallbacks per call.
func (o *runtimeOptions) t(key string) string {
	return o.catalog.Get(key)
}

type runtime struct {
	store              *sqlite.Store
	configPath         string
	dbPath             string
	repoLocalDir       string
	bus                events.Bus
	hooksEngine        *hooks.Engine
	notificationAction *actions.NotificationShowAction
	// cache holds the project runtime seeded for this CLI invocation.
	cache *agentruntime.BundleCache
	// projectID is the cache key for the boot-seeded entry — 0 when no
	// --project flag was supplied (the default fallback used by every
	// pre-3c command). ResolveProjectRuntime uses this as the lookup
	// key when callers do not specify their own.
	projectID int64
}

func (r *runtime) WithActivityRepo(ctx context.Context) context.Context {
	return activity.WithRepository(ctx, r.store)
}

// close swallows the close error after logging the intent — every CLI command
// uses `defer rt.close()` instead of inlining `defer func() { _ = rt.store.Close() }()`
// so the boilerplate stays in one place.
func (r *runtime) close() {
	if r.cache != nil {
		_ = r.cache.Close()
	}
	_ = r.store.Close()
}

// operationService returns the boot-seeded operation.Service from the
// active ProjectRuntime. Read-only catalog CLIs (template/law/skill/
// persona list+show, workflow show/orphans) go through this facade
// rather than constructing parallel app.*Service instances.
func (r *runtime) operationService() *operation.Service {
	return r.ProjectRuntime().Service.ForCLI()
}

// activeRegistry returns the EnumRegistry from the BundleCache's active
// ProjectRuntime, falling back to the boot-time registry field for
// non-cache code paths (tests that skip materializeConfig, the bootstrap
// window between sqlite.Open and cache.Install). Centralising the
// lookup means every service helper goes through the cache transparently
// without churn at every callsite.
func (r *runtime) activeRegistry() *domain.EnumRegistry {
	if pr := r.ProjectRuntime(); pr != nil {
		return pr.EnumRegistry
	}
	return nil
}

// activeSnapshot returns the per-project *config.Snapshot from the
// boot-seeded ProjectRuntime. App services constructed inside CLI
// subcommands capture this pointer once at construction; the same
// pointer survives the lifetime of the CLI invocation because the cache
// only rotates on mtime change, which the single-shot CLI does not
// observe mid-call. Returns nil when no cache entry exists (rare
// bootstrap window — callers that touch the snapshot must guard).
func (r *runtime) activeSnapshot() *config.Snapshot {
	pr := r.ProjectRuntime()
	if pr == nil {
		return nil
	}
	return pr.Snapshot
}

// Runners are the delivery adapters the executable injects; the CLI imports
// none of their implementations.
type Runners struct {
	Interactive func(context.Context, agentruntime.Session) error
}

func NewRootCommand(version string, runners Runners) *cobra.Command {
	ensurePkgCatalog()
	opts := &runtimeOptions{catalog: pkgCatalog}
	cmd := &cobra.Command{
		Use:           "okt",
		Short:         opts.t("cli.root.short"),
		Long:          opts.t("cli.root.long"),
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	configureRootFlags(cmd, opts)
	addRootCommands(cmd, opts, version, runners)
	cmd.MarkFlagsMutuallyExclusive("project", "project-id")
	configureCommandTree(cmd)
	return cmd
}

func configureRootFlags(cmd *cobra.Command, opts *runtimeOptions) {
	cmd.PersistentFlags().StringVar(&opts.dbPath, "db", "", opts.t("cli.root.flag.db"))
	cmd.PersistentFlags().StringVar(&opts.configPath, "config", "", opts.t("cli.root.flag.config"))
	cmd.PersistentFlags().StringVarP(&opts.project, "project", "p", "", opts.t("cli.root.flag.project"))
	cmd.PersistentFlags().Int64Var(&opts.projectID, "project-id", 0, opts.t("cli.root.flag.project-id"))
	_ = cmd.MarkPersistentFlagFilename("config", "yaml", "yml")
	_ = cmd.MarkPersistentFlagFilename("db", "db")
}

func addRootCommands(cmd *cobra.Command, opts *runtimeOptions, version string, runners Runners) {
	cmd.AddCommand(newInitCommand(opts))
	cmd.AddCommand(newListCommand(opts))
	cmd.AddCommand(newMoveCommand(opts))
	cmd.AddCommand(newAssignCommand(opts))
	cmd.AddCommand(newEditCommand(opts))
	cmd.AddCommand(newDeleteCommand(opts))
	cmd.AddCommand(newArchiveCommand(opts))
	cmd.AddCommand(newUnarchiveCommand(opts))
	cmd.AddCommand(newCommentCommand(opts))
	cmd.AddCommand(newDependCommand(opts))
	cmd.AddCommand(newPlanCommand(opts))
	cmd.AddCommand(newPresetCommand(opts))
	cmd.AddCommand(newLogsCommand(opts))
	cmd.AddCommand(newWorkflowCommand(opts))
	cmd.AddCommand(newConfigCommand(opts))
	cmd.AddCommand(newDBCommand(opts))
	cmd.AddCommand(newInsightsCommand(opts))
	cmd.AddCommand(newProgressCommand(opts))
	cmd.AddCommand(newTaskCommand(opts))
	cmd.AddCommand(newMetricsCommand(opts))
	cmd.AddCommand(newErrorCommand(opts))
	cmd.AddCommand(newSolutionCommand(opts))
	cmd.AddCommand(newProjectCommand(opts))
	cmd.AddCommand(newProjectsCommand(opts))
	cmd.AddCommand(newLawCommand(opts))
	cmd.AddCommand(newSkillCommand(opts))
	cmd.AddCommand(newPersonaCommand(opts))
	cmd.AddCommand(newTemplateCommand(opts))
	cmd.AddCommand(newSearchCommand(opts))
	cmd.AddCommand(newKnowledgeCommand(opts))
	cmd.AddCommand(newTagCommand(opts))
	cmd.AddCommand(newTUICommand(opts, version, runners.Interactive))
	cmd.AddCommand(newCommandCommand(opts))
	cmd.AddCommand(newSetupCommand(opts))
	cmd.AddCommand(newUninstallCommand(opts))
	cmd.AddCommand(newUpdateCommand(opts))
}

func (o *runtimeOptions) open(ctx context.Context, materializeConfig bool) (*runtime, error) {
	dbPath, err := o.resolvedDBPath()
	if err != nil {
		return nil, err
	}

	store, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		return nil, err
	}

	repoLocalDir, err := o.resolveRepoLocalDir(ctx, store)
	if err != nil {
		_ = store.Close()
		return nil, err
	}

	cs := configstore.New()
	if materializeConfig {
		if err := o.prepareConfig(cs); err != nil {
			_ = store.Close()
			return nil, err
		}
	}

	configPath, err := o.resolvedConfigPath()
	if err != nil {
		_ = store.Close()
		return nil, err
	}

	rt := &runtime{store: store, configPath: configPath, dbPath: dbPath, repoLocalDir: repoLocalDir}

	if materializeConfig {
		if err := rt.materialize(ctx, o, cs); err != nil {
			_ = store.Close()
			return nil, err
		}
	}

	return rt, nil
}

func (o *runtimeOptions) resolveRepoLocalDir(ctx context.Context, store *sqlite.Store) (string, error) {
	var err error
	o.discoveryStart, err = o.resolveDiscoveryStart(ctx, store)
	if err != nil {
		return "", err
	}
	repoLocalDir, err := o.discoverRepoLocalRoot()
	if err != nil {
		return "", err
	}
	if o.configPath != "" {
		// --config overrides discovery, including the runtime's local badge.
		repoLocalDir = ""
	}
	return repoLocalDir, nil
}

func (o *runtimeOptions) prepareConfig(store *configstore.Adapter) error {
	rootDir, err := o.resolvedConfigRoot()
	if err != nil {
		return err
	}
	return store.EnsureDefaultFiles(rootDir)
}

func (r *runtime) materialize(ctx context.Context, opts *runtimeOptions, cs *configstore.Adapter) error {
	preview, err := config.LoadBundle(r.configPath)
	if err != nil {
		firstKind := classifyValidationError(err)
		return domain.NewError(
			domain.ErrConfigInvalid,
			fmt.Sprintf(t("cli.tui.err.config_validation_failed_fmt"), 1, firstKind),
			buildValidateFailureDetails(r.configPath, err, nil),
		)
	}
	emitBundleWarnings(preview)

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	cache, bus, pr, err := agentruntime.Bootstrap(ctx, r.store, cs, r.configPath, contract.ProjectSelector{ProjectID: opts.projectID, Project: opts.project, CWD: cwd}, preview)
	if err != nil {
		return err
	}
	r.bus = bus
	r.hooksEngine = pr.HooksEngine
	r.notificationAction = pr.NotificationAction
	r.cache = cache
	r.projectID = opts.projectID
	return nil
}

// ProjectRuntime returns the active boot-seeded ProjectRuntime. Panics
// when the runtime was opened with materializeConfig=false (rare boot
// shape that skips bundle inflation) — callers always reach this from
// a subcommand that requires a wired runtime.
func (r *runtime) ProjectRuntime() *agentruntime.ProjectRuntime {
	if r.cache == nil {
		return nil
	}
	return r.cache.Get(r.projectID)
}

func (o *runtimeOptions) resolvedConfigPath() (string, error) {
	if o.configPath != "" {
		return filepath.Abs(o.configPath)
	}
	if repoLocal, err := o.discoverRepoLocalRoot(); err != nil {
		return "", err
	} else if repoLocal != "" {
		return paths.ActiveConfigFileInDir(filepath.Join(repoLocal, "config"))
	}
	return paths.ConfigFile()
}

// resolvedConfigRoot returns the directory EnsureDefaultFiles operates on.
// Resolution order:
//  1. --config flag (root derived from the yaml path).
//  2. Walk-up `.omakiten/` discovery (becomes the standalone install root —
//     no merge with the user-global ConfigRoot).
//  3. XDG / OMAKITEN_HOME default.
func (o *runtimeOptions) resolvedConfigRoot() (string, error) {
	if o.configPath != "" {
		abs, err := filepath.Abs(o.configPath)
		if err != nil {
			return "", err
		}
		return config.ConfigRootFromYAMLPath(abs), nil
	}
	if repoLocal, err := o.discoverRepoLocalRoot(); err != nil {
		return "", err
	} else if repoLocal != "" {
		return repoLocal, nil
	}
	return paths.ConfigRoot()
}

// discoverRepoLocalRoot walks up from o.discoveryStart looking for
// `.omakiten/`. Returns the absolute path of the first hit, or "" when no
// install is found before the walker hits $HOME / the filesystem root.
//
// discoveryStart is populated by open() so the walk respects --project (the
// project's root_path) when set. When the field is empty (callers that
// resolve config before open(), e.g. `okt config validate`), the walker
// falls back to the current working directory.
//
// --config explicitly overrides discovery — callers must not consult this
// helper when the flag is supplied.
func (o *runtimeOptions) discoverRepoLocalRoot() (string, error) {
	start := o.discoveryStart
	if start == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		start = cwd
	}
	dir, ok, err := config.FindRepoLocal(start)
	if err != nil || !ok {
		return "", err
	}
	return dir, nil
}

// resolveDiscoveryStart returns the directory FindRepoLocal should walk up
// from. When --project / --project-id is supplied, looks up the project's
// root_path in the DB and uses it. Anything that prevents the lookup (no
// such project, store error) falls back to CWD without aborting open() —
// the user-flag-but-no-project case still gets a working runtime, the
// project resolution will surface the real error later when the command
// actually needs the project context.
func (o *runtimeOptions) resolveDiscoveryStart(ctx context.Context, store *sqlite.Store) (string, error) {
	if o.project == "" && o.projectID == 0 {
		return os.Getwd()
	}
	resolver := projectresolver.NewResolver(store)
	cwd, _ := os.Getwd()
	project, err := resolver.Resolve(ctx, projectresolver.ResolveOptions{ProjectID: o.projectID, Project: o.project, CWD: cwd})
	if err != nil {
		// The user explicitly named a project (--project / --project-id);
		// silently degrading to CWD masks the resolution failure and
		// drops the rest of the command on a different bundle than the
		// caller asked for. Surface the typed error so the operator
		// can fix the flag or create the project before retrying.
		return "", err
	}
	if project.RootPath == "" {
		return cwd, nil
	}
	return project.RootPath, nil
}

func (o *runtimeOptions) resolvedDBPath() (string, error) {
	if o.dbPath != "" {
		return filepath.Abs(o.dbPath)
	}
	return paths.DatabaseFile()
}

func (o *runtimeOptions) resolveProject(ctx context.Context, store *sqlite.Store) (domain.ProjectContext, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return domain.ProjectContext{}, err
	}

	resolver := projectresolver.NewResolver(store)
	return resolver.Resolve(ctx, projectresolver.ResolveOptions{ProjectID: o.projectID, Project: o.project, CWD: cwd})
}

func writeSuccess(cmd *cobra.Command, data any) error {
	return output.Write(cmd.OutOrStdout(), output.Success(data))
}

func writeError(cmd *cobra.Command, err error) error {
	envelope := commandFailure(cmd, err)
	if streamIsTTY(cmd.ErrOrStderr()) {
		printCommandFailure(cmd.ErrOrStderr(), envelope)
	}
	_ = output.Write(cmd.OutOrStdout(), envelope)
	return exitError{code: 1}
}

func commandFailure(cmd *cobra.Command, err error) output.Envelope {
	code, message, details := "internal_error", err.Error(), map[string]any(nil)
	var coded *domain.CodedError
	if errors.As(err, &coded) {
		code, message, details = string(coded.Code), coded.Message, coded.Details
	} else if coded = codedFromOperationDenied(err); coded != nil {
		code, message, details = string(coded.Code), coded.Message, coded.Details
	}
	details = failureGuidance(cmd, code, details)
	return output.Failure(code, message, details)
}

// codedFromOperationDenied maps a surfaces deny onto the named
// operation_denied envelope so every runJSON command that already
// returns the facade error is covered without touching cobra files.
func codedFromOperationDenied(err error) *domain.CodedError {
	var denied operation.OperationDenied
	if !errors.As(err, &denied) {
		return nil
	}
	ensurePkgCatalog()
	return denied.Coded(pkgCatalog)
}

// emitBundleWarnings surfaces non-fatal config issues (skipped custom
// notifications, slug↔frontmatter drift, etc.) on stderr at startup so the
// user sees them on `okt init` / `okt tui` / any CLI command without
// having to inspect bundle.Warnings programmatically. Silent when the
// bundle is clean.
func emitBundleWarnings(bundle config.Bundle) {
	for _, w := range bundle.Warnings {
		switch {
		case w.Path != "" && w.Slug != "":
			fmt.Fprintf(os.Stderr, t("cli.print.warn_path_slug"), w.Path, w.Slug, w.Message)
		case w.Path != "":
			fmt.Fprintf(os.Stderr, t("cli.print.warn_path"), w.Path, w.Message)
		case w.Slug != "":
			fmt.Fprintf(os.Stderr, t("cli.print.warn_slug"), w.Slug, w.Message)
		default:
			fmt.Fprintf(os.Stderr, t("cli.print.warn_message"), w.Message)
		}
	}
}

func runJSON(cmd *cobra.Command, fn func(context.Context) (any, error)) error {
	ctx := activity.WithAgent(cmd.Context(), "cli", cmd.CommandPath(), os.Getenv("OMAKITEN_AGENT_MODEL"), os.Getenv("OMAKITEN_AGENT_SESSION_ID"))
	data, err := fn(ctx)
	if err != nil {
		return writeError(cmd, err)
	}
	if err := writeSuccess(cmd, data); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}
