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
	store       *sqlite.Store
	configPath  string
	dbPath      string
	bus         events.Bus
	hooksEngine *hooks.Engine
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
	return app.NewLawService(r.store, r.bundleEditor(), store, store)
}

func (r *runtime) personaService() *app.PersonaService {
	store := configstore.New()
	return app.NewPersonaService(r.store, r.bundleEditor(), store, store)
}

func (r *runtime) contextService() *app.ContextService {
	return app.NewContextService(r.store, r.store, r.store, r.store, r.store, r.tokenCounter())
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
  2. $OMAKITEN_HOME — pins config to <HOME>/config/omakiten.yaml and data to <HOME>/data/omakiten.db
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
	configPath, err := o.resolvedConfigPath()
	if err != nil {
		return nil, err
	}
	dbPath, err := o.resolvedDBPath()
	if err != nil {
		return nil, err
	}

	cs := configstore.New()
	if materializeConfig {
		rootDir := cs.ConfigRootFromYAMLPath(configPath)
		if err := cs.MigrateLayout(rootDir); err != nil {
			return nil, err
		}
		if err := cs.EnsureDefaultFiles(rootDir); err != nil {
			return nil, err
		}
	}

	store, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		return nil, err
	}

	rt := &runtime{store: store, configPath: configPath, dbPath: dbPath}

	if materializeConfig {
		// Import loads + validates + populates the domain registries
		// (priority/severity) BEFORE writing to SQLite, so the rest
		// of the CLI invocation sees a fully wired runtime. The
		// registries live for the duration of the process.
		bundle, _, err := app.NewConfigService(store, cs).Import(ctx, configPath)
		if err != nil {
			_ = store.Close()
			return nil, err
		}

		bus := events.NewInProcessBus(bundle.Config.Events)
		registry := hooks.NewActionRegistry()
		actions.RegisterBuiltins(registry)
		if err := config.ValidateHooks(bundle.Config.Hooks, func(name string) bool {
			_, ok := registry.Get(name)
			return ok
		}); err != nil {
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

		hookEntries := make([]hooks.Hook, 0, len(bundle.Config.Hooks))
		for _, spec := range bundle.Config.Hooks {
			hookEntries = append(hookEntries, hooks.Hook{On: spec.On, When: spec.When, Do: spec.Do, Args: spec.Args})
		}
		engine := hooks.NewEngine(hookEntries, registry, bundle.Config.Events, store)
		engine.Start(bus)
		rt.bus = bus
		rt.hooksEngine = engine
	}

	return rt, nil
}

func (o *runtimeOptions) resolvedConfigPath() (string, error) {
	if o.configPath != "" {
		return filepath.Abs(o.configPath)
	}
	return paths.ConfigFile()
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
