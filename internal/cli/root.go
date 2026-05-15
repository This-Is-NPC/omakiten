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
	return app.NewBundleEditor(r.store, configstore.New(), r.configPath)
}

func (r *runtime) skillService() *app.SkillService {
	store := configstore.New()
	return app.NewSkillService(r.store, r.bundleEditor(), store, store)
}

func (r *runtime) lawService() *app.LawService {
	store := configstore.New()
	return app.NewLawService(r.store, r.bundleEditor(), store, store, r.registry)
}

func (r *runtime) personaService() *app.PersonaService {
	store := configstore.New()
	return app.NewPersonaService(r.store, r.bundleEditor(), store, store)
}

func (r *runtime) contextService() *app.ContextService {
	return app.NewContextService(r.store, r.store, r.store, r.store, r.store, r.tokenCounter(), r.registry)
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

	cs := configstore.New()
	if materializeConfig {
		rootDir, err := o.resolvedConfigRoot()
		if err != nil {
			return nil, err
		}
		if err := cs.MigrateLayout(rootDir); err != nil {
			return nil, err
		}
		if err := cs.EnsureDefaultFiles(rootDir); err != nil {
			return nil, err
		}
	}

	// Resolve configPath AFTER MigrateLayout has had a chance to relocate
	// renamed kits — otherwise a snapshot taken pre-migration points at
	// the just-moved root copy and Import fails with ENOENT. Non-materialize
	// callers (e.g. `okt config validate`) skip migration and accept the raw
	// resolver output.
	configPath, err := o.resolvedConfigPath()
	if err != nil {
		return nil, err
	}

	store, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		return nil, err
	}

	repoLocalDir, err := o.discoverRepoLocalRoot()
	if err != nil {
		return nil, err
	}
	if o.configPath != "" {
		// --config overrides discovery — the TUI badge must reflect what the
		// runtime is actually loading, not a discovered .omakiten/ that the
		// flag bypassed.
		repoLocalDir = ""
	}

	rt := &runtime{store: store, configPath: configPath, dbPath: dbPath, repoLocalDir: repoLocalDir}

	if materializeConfig {
		// Import loads + validates + populates the domain registries
		// (priority/severity) BEFORE writing to SQLite, so the rest
		// of the CLI invocation sees a fully wired runtime. The
		// registries live for the duration of the process.
		bundle, _, enumRegistry, err := app.NewConfigService(store, cs).Import(ctx, configPath)
		if err != nil {
			_ = store.Close()
			return nil, err
		}
		emitBundleWarnings(bundle)
		rt.registry = enumRegistry

		bus := events.NewInProcessBus(bundle.Config.Events)
		registry := hooks.NewActionRegistry()
		actions.RegisterBuiltins(registry)
		notificationAction := actions.NewNotificationShowAction(notificationSnapshotFromBundle(bundle))
		registry.Register(notificationAction)
		rt.notificationAction = notificationAction
		if err := config.ValidateHooks(bundle.Config.Hooks, func(name string) bool {
			_, ok := registry.Get(name)
			return ok
		}, bundle.Notifications); err != nil {
			_ = store.Close()
			return nil, err
		}

		// Reapply busy_timeout with the user-resolved value, then wire the
		// activity-log + events knobs into the live Store. Mirrors the
		// agentruntime composition root — every entry point that opens a
		// Store from the user's bundle goes through this step.
		if err := store.ApplyConfig(ctx, sqlite.ConfigKnobs{
			BusyTimeoutMs:            bundle.Config.SQLite.BusyTimeoutMs,
			ActivityLogMaxRows:       bundle.Config.ActivityLog.MaxRows,
			ActivityLogMaxAgeDays:    bundle.Config.ActivityLog.MaxAgeDays,
			EventsDefaultRecentLimit: bundle.Config.Events.DefaultRecentLimit,
			EventsPolicy:             bundle.Config.Events,
			EventBus:                 bus,
		}); err != nil {
			_ = store.Close()
			return nil, err
		}
		// Tag synonyms + similar-task stopwords flow through process-global
		// registries the leaf helpers read at every call. Composition root
		// is the single point that knows the bundle, so the install lives
		// here.
		app.RegisterTagSynonyms(bundle.Config.TagSynonyms)
		agent.RegisterStopWords(bundle.Config.Search.Stopwords)

		hookEntries := buildHookEntries(bundle.Config.Hooks)
		engine := hooks.NewEngine(hookEntries, registry, bundle.Config.Events, store)
		engine.Start(bus)
		rt.bus = bus
		rt.hooksEngine = engine
	}

	return rt, nil
}

// buildHookEntries lifts user-facing HookSpec entries into the
// engine's hooks.Hook shape. Notification-shape entries (HookSpec.Notification
// non-empty) are rewritten to call the notification.show action with the
// slug stashed under actions.NotificationArgSlug. Optional hook-level
// `message:` / `message_field:` overrides ride along under their
// own arg keys so the action can fall back to them when the
// referenced notification YAML does not declare its own message source.
func buildHookEntries(specs []config.HookSpec) []hooks.Hook {
	out := make([]hooks.Hook, 0, len(specs))
	for _, spec := range specs {
		if spec.Notification != "" {
			out = append(out, hooks.Hook{
				On:   spec.On,
				When: spec.When,
				Do:   actions.NotificationActionName,
				Args: map[string]any{
					actions.NotificationArgSlug:               spec.Notification,
					actions.NotificationArgMessage:            spec.Message,
					actions.NotificationArgMessageField:       spec.MessageField,
					actions.NotificationArgDetailMessage:      spec.DetailMessage,
					actions.NotificationArgDetailMessageField: spec.DetailMessageField,
				},
			})
			continue
		}
		out = append(out, hooks.Hook{On: spec.On, When: spec.When, Do: spec.Do, Args: spec.Args})
	}
	return out
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

// discoverRepoLocalRoot walks up from the CWD looking for `.omakiten/`. Returns
// the absolute path of the first hit, or "" when no overlay is found before
// the walker hits $HOME / the filesystem root. Stat errors are surfaced so
// callers can decide whether to abort or fall back to the global resolver.
//
// When the user supplies --config explicitly, callers must not consult this
// helper — the flag is the authoritative override.
func (o *runtimeOptions) discoverRepoLocalRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir, ok, err := config.FindRepoLocal(cwd)
	if err != nil || !ok {
		return "", err
	}
	return dir, nil
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

// notificationSnapshotFromBundle builds the slim view of the bundle that the
// notification.show action consults at execute time.
func notificationSnapshotFromBundle(bundle config.Bundle) actions.NotificationBundleSnapshot {
	return actions.NotificationBundleSnapshot{Notifications: bundle.Notifications}
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
