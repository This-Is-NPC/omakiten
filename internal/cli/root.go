package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"omakiten/internal/activity"
	"omakiten/internal/agent"
	"omakiten/internal/agentruntime"
	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/configstore"
	"omakiten/internal/domain"
	"omakiten/internal/events"
	"omakiten/internal/hooks"
	"omakiten/internal/hooks/actions"
	"omakiten/internal/output"
	"omakiten/internal/paths"
	projectresolver "omakiten/internal/project"
	"omakiten/internal/sqlite"
	"omakiten/internal/token"
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
}

type runtime struct {
	store              *sqlite.Store
	configPath         string
	dbPath             string
	repoLocalDir       string
	bus                events.Bus
	hooksEngine        *hooks.Engine
	notificationAction *actions.NotificationShowAction
	registry           *domain.EnumRegistry
	// cache is the per-project BundleCache the CLI invocation seeds at
	// boot. Phase 3c keeps the cache size at 1 for the single-shot CLI
	// path (one --project per invocation), but exposing the cache lets
	// MCP-style multi-project surfaces re-resolve through the same
	// handle as the agentruntime composition root. Subcommands that
	// want a project-aware bundle call ResolveProjectRuntime.
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
	if r.hooksEngine != nil {
		r.hooksEngine.Stop()
	}
	_ = r.store.Close()
}

// bundleEditor builds the editor the way every config-touching service expects
// it. Centralising this lets the call sites stay one line each.
func (r *runtime) bundleEditor() *app.BundleEditor {
	return app.NewBundleEditor(configstore.New(), r.configPath)
}

func (r *runtime) skillService() *app.SkillService {
	store := configstore.New()
	return app.NewSkillService(r.activeSnapshot(), r.bundleEditor(), store, store)
}

func (r *runtime) lawService() *app.LawService {
	store := configstore.New()
	return app.NewLawService(r.activeSnapshot(), r.bundleEditor(), store, store, r.activeRegistry())
}

func (r *runtime) personaService() *app.PersonaService {
	store := configstore.New()
	return app.NewPersonaService(r.activeSnapshot(), r.bundleEditor(), store, store)
}

func (r *runtime) contextService() *app.ContextService {
	return app.NewContextService(r.store, r.store, r.store, r.store, r.activeSnapshot(), r.tokenCounter(), r.activeRegistry())
}

// commentService wraps NewCommentService and threads the per-project
// tag synonyms into the service via SetSynonyms. CLI inline callers
// use this instead of app.NewCommentService(rt.store) so Phase 3f's
// per-project lookup flows naturally.
func (r *runtime) commentService() *app.CommentService {
	svc := app.NewCommentService(r.store)
	svc.SetSynonyms(r.activeSynonyms())
	return svc
}

// commentServiceWithWorkflow mirrors commentService for the edit /
// remove flows that need workflow policy enforcement.
func (r *runtime) commentServiceWithWorkflow(workflow *app.WorkflowService) *app.CommentService {
	svc := app.NewCommentServiceWithWorkflow(r.store, workflow)
	svc.SetSynonyms(r.activeSynonyms())
	return svc
}

// activeRegistry returns the EnumRegistry from the BundleCache's active
// ProjectRuntime, falling back to the boot-time registry field for
// non-cache code paths (tests that skip materializeConfig, the bootstrap
// window between sqlite.Open and cache.Install). Centralising the
// lookup means every service helper goes through the cache transparently
// without churn at every callsite.
func (r *runtime) activeRegistry() *domain.EnumRegistry {
	if pr := r.ProjectRuntime(); pr != nil && pr.EnumRegistry != nil {
		return pr.EnumRegistry
	}
	return r.registry
}

// activeSynonyms returns the per-project tag synonym table from the
// active ProjectRuntime's Snapshot. Phase 3f wires this into
// TagService / CommentService / ErrorService at construction time so
// NormalizeTagName resolves the active project's aliases rather than a
// process-global registry. Phase 2-bis routes the read through
// pr.Snapshot.Synonyms() so the synonyms always reflect the same
// immutable bundle view the agent service observes.
// Returns nil when no cache entry exists yet (rare bootstrap window).
func (r *runtime) activeSynonyms() map[string]string {
	pr := r.ProjectRuntime()
	if pr == nil || pr.Snapshot == nil {
		return nil
	}
	return pr.Snapshot.Synonyms()
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


func (r *runtime) tokenCounter() token.Counter {
	return token.NewCounter()
}

func NewRootCommand(version string) *cobra.Command {
	opts := &runtimeOptions{}
	cmd := &cobra.Command{
		Use:   "okt",
		Short: "Opinionated checkpoints for AI-driven development",
		Long: `okt drives Omakiten from the command line and TUI.

Path resolution (highest to lowest precedence):
  1. --config / --db flags
  2. $OMAKITEN_HOME — pins config to <HOME>/config/<active>.yaml and data to <HOME>/data/omakiten.db
  3. $XDG_CONFIG_HOME / $XDG_DATA_HOME
  4. ~/.config/omakiten and ~/.local/share/omakiten`,
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.PersistentFlags().StringVar(&opts.dbPath, "db", "", "SQLite database path (overrides $OMAKITEN_HOME and XDG)")
	cmd.PersistentFlags().StringVar(&opts.configPath, "config", "", "omakiten.yaml path (overrides $OMAKITEN_HOME and XDG)")
	cmd.PersistentFlags().StringVarP(&opts.project, "project", "p", "", "project slug")
	cmd.PersistentFlags().Int64Var(&opts.projectID, "project-id", 0, "project id")

	cmd.AddCommand(newInitCommand(opts))
	cmd.AddCommand(newAddCommand(opts))
	cmd.AddCommand(newListCommand(opts))
	cmd.AddCommand(newMoveCommand(opts))
	cmd.AddCommand(newEditCommand(opts))
	cmd.AddCommand(newDeleteCommand(opts))
	cmd.AddCommand(newArchiveCommand(opts))
	cmd.AddCommand(newUnarchiveCommand(opts))
	cmd.AddCommand(newCommentCommand(opts))
	cmd.AddCommand(newDependCommand(opts))
	cmd.AddCommand(newContextCommand(opts))
	cmd.AddCommand(newWorkflowCommand(opts))
	cmd.AddCommand(newConfigCommand(opts))
	cmd.AddCommand(newLawCommand(opts))
	cmd.AddCommand(newSkillCommand(opts))
	cmd.AddCommand(newPersonaCommand(opts))
	cmd.AddCommand(newTUICommand(opts, version))
	cmd.AddCommand(newMCPCommand(opts))

	return cmd
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

	// Project-aware discovery: when --project / --project-id is supplied,
	// walk-up starts at the project's root_path (looked up from the DB)
	// instead of the CWD. This lets `okt --project B cmd` from CWD=A pick
	// up B's .omakiten/ even if A also has one. Unresolvable project flags
	// degrade to CWD-based discovery rather than aborting.
	o.discoveryStart, err = o.resolveDiscoveryStart(ctx, store)
	if err != nil {
		_ = store.Close()
		return nil, err
	}

	repoLocalDir, err := o.discoverRepoLocalRoot()
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	if o.configPath != "" {
		// --config overrides discovery — the TUI badge must reflect what
		// the runtime is actually loading, not a discovered .omakiten/
		// that the flag bypassed.
		repoLocalDir = ""
	}

	cs := configstore.New()
	if materializeConfig {
		rootDir, err := o.resolvedConfigRoot()
		if err != nil {
			_ = store.Close()
			return nil, err
		}
		if err := cs.MigrateLayout(rootDir); err != nil {
			_ = store.Close()
			return nil, err
		}
		if err := cs.EnsureDefaultFiles(rootDir); err != nil {
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
		// Single construction path: peek the bundle once for the events
		// bus seed (the bus must outlive every cache rebuild), then
		// delegate every other per-bundle wire (registry, hooks
		// engine, notification snapshot, synonyms, stopwords) to the
		// shared agentruntime.BuildProjectRuntime via cache.Resolve.
		// Mirrors the MCP composition root so CLI and MCP cannot
		// drift on what "boot" produces.
		preview, err := config.LoadBundle(configPath)
		if err != nil {
			_ = store.Close()
			return nil, err
		}
		emitBundleWarnings(preview)

		cwd, err := os.Getwd()
		if err != nil {
			_ = store.Close()
			return nil, err
		}
		bus := events.NewInProcessBus(preview.Config.Events)
		cache := agentruntime.NewBundleCache(store, bus, cs)
		cache.SetProjectSelector(agent.ProjectSelector{ProjectID: o.projectID, Project: o.project, CWD: cwd})

		pr, err := cache.Resolve(ctx, o.projectID, configPath)
		if err != nil {
			_ = store.Close()
			return nil, err
		}

		rt.registry = pr.EnumRegistry
		rt.bus = bus
		rt.hooksEngine = pr.HooksEngine
		rt.notificationAction = pr.NotificationAction
		rt.cache = cache
		rt.projectID = o.projectID
	}

	return rt, nil
}

// ResolveProjectRuntime returns the ProjectRuntime for the supplied
// project selector, consulting the BundleCache the runtime seeded at
// boot. When selector zero, returns the entry that was Installed for
// the active --project (or the default 0 key when no flag was
// supplied). Used by subcommands that want a project-aware bundle
// handle without re-implementing the cache lookup.
func (r *runtime) ResolveProjectRuntime(ctx context.Context, projectID int64) (*agentruntime.ProjectRuntime, error) {
	if r.cache == nil {
		return nil, fmt.Errorf("cli runtime: bundle cache is not initialised; open() must run with materializeConfig=true")
	}
	if projectID == 0 {
		projectID = r.projectID
	}
	if pr := r.cache.Get(projectID); pr != nil {
		return pr, nil
	}
	// Fall back to the boot-seeded entry — Phase 3c does not yet
	// reparse a different project's bundle from the subcommand surface;
	// that arrives in Phase 3e/3f. Returning the active entry keeps the
	// surface uniform for callers.
	if pr := r.cache.Get(r.projectID); pr != nil {
		return pr, nil
	}
	return nil, fmt.Errorf("cli runtime: no ProjectRuntime cached for project %d", projectID)
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

// resolvedConfigRoot returns the directory MigrateLayout / EnsureDefaultFiles
// operate on. Resolution order:
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
func (o *runtimeOptions) resolveDiscoveryStart(ctx context.Context, store app.ProjectRepository) (string, error) {
	if o.project == "" && o.projectID == 0 {
		return os.Getwd()
	}
	resolver := projectresolver.NewResolver(store)
	cwd, _ := os.Getwd()
	project, err := resolver.Resolve(ctx, projectresolver.ResolveOptions{ProjectID: o.projectID, Project: o.project, CWD: cwd})
	if err != nil || project.RootPath == "" {
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

func (o *runtimeOptions) resolveProject(ctx context.Context, store app.ProjectRepository) (domain.ProjectContext, error) {
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
	var coded *domain.CodedError
	if errors.As(err, &coded) {
		_ = output.Write(cmd.OutOrStdout(), output.Failure(string(coded.Code), coded.Message, coded.Details))
		return exitError{code: 1}
	}

	_ = output.Write(cmd.OutOrStdout(), output.Failure("internal_error", err.Error(), nil))
	return exitError{code: 1}
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
			fmt.Fprintf(os.Stderr, "warning: %s [%s]: %s\n", w.Path, w.Slug, w.Message)
		case w.Path != "":
			fmt.Fprintf(os.Stderr, "warning: %s: %s\n", w.Path, w.Message)
		case w.Slug != "":
			fmt.Fprintf(os.Stderr, "warning: [%s]: %s\n", w.Slug, w.Message)
		default:
			fmt.Fprintf(os.Stderr, "warning: %s\n", w.Message)
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
