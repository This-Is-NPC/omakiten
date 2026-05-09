// Package agentruntime is the composition root for the Omakiten agent. It
// owns the bootstrap that wires the sqlite store, the configstore adapter,
// the agent service, and the per-bundle template/lookup snapshots. By
// living here (rather than inside `internal/agent`), the agent package
// itself stays free of `internal/config`, `internal/paths`, and
// `internal/sqlite` imports — the agent only knows about the inward-facing
// service+DTO model.
package agentruntime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"omakiten/internal/agent"
	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/configstore"
	"omakiten/internal/events"
	"omakiten/internal/hooks"
	"omakiten/internal/hooks/actions"
	"omakiten/internal/paths"
	"omakiten/internal/sqlite"
	"omakiten/internal/tui/components/buddy"
)

// registerPriorities/registerSeverities used to live here. They were
// hoisted into app.ConfigService.Import (which runs at every bundle
// load) so the registry is populated before any path that consumes
// it — including ImportBundle's own resolve-label-to-id step.

// Options mirrors agent.Open's old signature so call sites only swap the
// import path.
type Options struct {
	DBPath     string
	ConfigPath string
	Project    string
	ProjectID  int64
	CWD        string
}

// Runtime owns the long-lived resources the MCP server needs: the sqlite
// connection, the resolved paths, and the agent.Service that handlers
// dispatch through.
type Runtime struct {
	store        *sqlite.Store
	configPath   string
	dbPath       string
	service      *agent.Service
	bus          events.Bus
	hooksEngine  *hooks.Engine
	actionRegistry *hooks.ActionRegistry
	buddyAction  *buddy.ShowAction
}

// Open materializes the runtime: resolves paths, runs config layout
// migration + default-file seeding, opens the sqlite store, imports the
// bundle, and wires the agent.Service with template snapshots.
func Open(ctx context.Context, opts Options) (*Runtime, error) {
	configPath, err := resolvedConfigPath(opts.ConfigPath)
	if err != nil {
		return nil, err
	}
	dbPath, err := resolvedDBPath(opts.DBPath)
	if err != nil {
		return nil, err
	}

	cs := configstore.New()
	rootDir := cs.ConfigRootFromYAMLPath(configPath)
	if err := cs.MigrateLayout(rootDir); err != nil {
		return nil, err
	}
	if err := cs.EnsureDefaultFiles(rootDir); err != nil {
		return nil, err
	}

	// Open the store before the bundle is parsed because ConfigService.Import
	// needs to write the bundle into SQLite. The kit-canonical busy_timeout
	// applied here covers the bootstrap window; once Import returns, we
	// reapply the user-resolved value via PRAGMA (per-connection) and wire
	// the activity-log + events knobs into the Store.
	store, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		return nil, err
	}

	bundle, _, err := app.NewConfigService(store, cs).Import(ctx, configPath)
	if err != nil {
		_ = store.Close()
		return nil, err
	}

	bus := events.NewInProcessBus(bundle.Config.Events)
	registry := hooks.NewActionRegistry()
	actions.RegisterBuiltins(registry)
	buddyAction := buddy.NewShowAction(buddy.BundleSnapshot{
		ActiveBuddy: bundle.Config.TUI.Buddy.Active,
		Buddies:     bundle.Buddies,
	})
	registry.Register(buddyAction)

	// Re-validate hooks now that the registry is populated so unknown
	// `do:` names abort startup with a clear error rather than silently
	// going untriggered.
	if err := config.ValidateHooks(bundle.Config.Hooks, func(name string) bool {
		_, ok := registry.Get(name)
		return ok
	}, buddyArgsValidator(bundle)); err != nil {
		_ = store.Close()
		return nil, err
	}

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

	cwd := opts.CWD
	if cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			_ = store.Close()
			return nil, err
		}
	}

	// Note: domain.RegisterPriorities / RegisterSeverities are called
	// inside ConfigService.Import (above) BEFORE ImportBundle writes
	// the bundle. No need to re-register here — the registry is
	// already populated for the rest of the runtime.

	hookEntries := make([]hooks.Hook, 0, len(bundle.Config.Hooks))
	for _, spec := range bundle.Config.Hooks {
		hookEntries = append(hookEntries, hooks.Hook{On: spec.On, When: spec.When, Do: spec.Do, Args: spec.Args})
	}
	engine := hooks.NewEngine(hookEntries, registry, bundle.Config.Events, store)
	engine.Start(bus)

	rt := &Runtime{store: store, configPath: configPath, dbPath: dbPath, bus: bus, hooksEngine: engine, actionRegistry: registry, buddyAction: buddyAction}
	rt.service = agent.NewService(store, agent.ProjectSelector{ProjectID: opts.ProjectID, Project: opts.Project, CWD: cwd})
	rt.service.SetTaskTemplateLookup(taskTemplateLookup(bundle))
	rt.service.SetTemplateCatalog(templateCatalog(bundle))
	rt.service.SetSkillCatalog(skillCatalog(bundle))
	rt.service.SetLawCatalog(lawCatalog(bundle))
	rt.service.SetPersonaCatalog(personaCatalog(bundle))
	rt.service.SetCommandCatalog(commandCatalog(bundle))
	// Validator guarantees every MCP field is declared in the loaded
	// bundle, so direct field access is safe — no Effective* fallback.
	// The *bool fields are dereferenced here because validator confirmed
	// they are non-nil.
	rt.service.SetSettings(agent.ServiceSettings{
		RecentCommentLimit:       bundle.Config.MCP.RecentCommentLimit,
		MaxCommentChars:          bundle.Config.MCP.MaxCommentChars,
		IncludeWorkflow:          *bundle.Config.MCP.IncludeWorkflowInContinue,
		CachePrompts:             *bundle.Config.MCP.CachePrompts,
		RecentContextLimit:       bundle.Config.MCP.RecentContextLimit,
		NextWorkLimit:            bundle.Config.MCP.NextWorkLimit,
		SimilarTaskLimit:         bundle.Config.MCP.SimilarTaskLimit,
		SolutionsTopLimitDefault: bundle.Config.Solutions.DefaultTopLimit,
		SolutionsTopLimitMax:     bundle.Config.Solutions.MaxTopLimit,
	})
	// Tag synonyms + similar-task stopwords are process-global registries
	// the consumer packages read at every NormalizeTagName / wordSet call.
	// The composition root is the single point that knows the bundle, so
	// installation lives here.
	app.RegisterTagSynonyms(bundle.Config.TagSynonyms)
	agent.RegisterStopWords(bundle.Config.Search.Stopwords)
	return rt, nil
}

func (r *Runtime) Close() error {
	if r.hooksEngine != nil {
		r.hooksEngine.Stop()
	}
	return r.store.Close()
}

// buddyArgsValidator returns the per-action arg validator the hooks
// validator invokes when do == "buddy.show". The closure is bound to
// the bundle so it can build the animation set from the active buddy
// (which the bundle validator separately enforces is non-empty when
// any hook does buddy.show).
func buddyArgsValidator(bundle config.Bundle) config.HookActionArgValidator {
	return func(action string, args map[string]any) error {
		if action != buddy.ActionName {
			return nil
		}
		active := bundle.Config.TUI.Buddy.Active
		if active == "" {
			return buddy.ValidateShowArgs(args, nil)
		}
		b, ok := bundle.Buddies[active]
		if !ok {
			return fmt.Errorf("buddy.show: config.tui.buddy.active=%q not loaded", active)
		}
		known := make(map[string]struct{}, len(b.Animations))
		for k := range b.Animations {
			known[k] = struct{}{}
		}
		return buddy.ValidateShowArgs(args, known)
	}
}

// ActionRegistry exposes the hook action registry so callers (TUI startup,
// tests) can register additional actions before the engine is busy.
func (r *Runtime) ActionRegistry() *hooks.ActionRegistry {
	return r.actionRegistry
}

// Bus returns the in-process events bus. Subscribers (live TUI panels,
// future buddies) register via this handle.
func (r *Runtime) Bus() events.Bus {
	return r.bus
}

func (r *Runtime) Service() *agent.Service {
	return r.service
}

func (r *Runtime) Store() *sqlite.Store {
	return r.store
}

func (r *Runtime) ConfigPath() string {
	return r.configPath
}

func (r *Runtime) DBPath() string {
	return r.dbPath
}

// templateCatalog snapshots the bundle's templates into the read-only view
// the MCP catalog endpoints expose. Snapshot at startup is enough — the
// agent is read-only and would only see drift after a config refresh, which
// requires restarting the runtime anyway.
func templateCatalog(bundle config.Bundle) agent.TemplateCatalog {
	snapshot := make([]agent.TemplateSummary, 0, len(bundle.Templates))
	for _, t := range bundle.Templates {
		snapshot = append(snapshot, agent.TemplateSummary{
			Slug:        t.Slug,
			Name:        t.Name,
			Description: t.Description,
			Entity:      t.Entity,
			Default:     t.Default,
			Project:     t.ProjectSlug,
			Laws:        append([]string(nil), t.Laws...),
			IsCustom:    t.IsCustom,
			Body:        t.Body,
			SourcePath:  t.SourcePath,
		})
	}
	return func() []agent.TemplateSummary {
		out := make([]agent.TemplateSummary, len(snapshot))
		copy(out, snapshot)
		return out
	}
}

// taskTemplateLookup captures the bundle at runtime startup and returns a
// project-aware closure that resolves the active task template scaffold on
// demand. Project-scoped templates win over global; nil means no template
// is configured for the kind.
func taskTemplateLookup(bundle config.Bundle) agent.TaskTemplateLookup {
	templates := append([]config.TaskTemplate(nil), bundle.Templates...)
	return func(projectSlug string) *agent.TaskTemplateSummary {
		var global *config.TaskTemplate
		for i := range templates {
			t := &templates[i]
			if t.Default != "task" {
				continue
			}
			if projectSlug != "" && t.ProjectSlug == projectSlug {
				return summarizeTaskTemplate(t)
			}
			if t.ProjectSlug == "" && global == nil {
				global = t
			}
		}
		if global == nil {
			return nil
		}
		return summarizeTaskTemplate(global)
	}
}

// skillCatalog/lawCatalog/personaCatalog/commandCatalog snapshot the bundle so
// agent.Service.ResolveCommand can resolve persona, skill, law and per-command
// bindings without importing internal/config. Snapshots are taken once at
// startup — same lifetime as templateCatalog — because a bundle reload
// requires restarting the runtime.

func skillCatalog(bundle config.Bundle) agent.SkillCatalog {
	snapshot := make([]agent.SkillInfo, 0, len(bundle.Skills))
	for _, s := range bundle.Skills {
		snapshot = append(snapshot, agent.SkillInfo{
			Slug:        s.Slug,
			Name:        s.Name,
			Description: s.Description,
			Body:        s.Body,
		})
	}
	return func() []agent.SkillInfo {
		out := make([]agent.SkillInfo, len(snapshot))
		copy(out, snapshot)
		return out
	}
}

func lawCatalog(bundle config.Bundle) agent.LawCatalog {
	snapshot := make([]agent.LawInfo, 0, len(bundle.Laws))
	for _, l := range bundle.Laws {
		snapshot = append(snapshot, agent.LawInfo{
			Slug:     l.Slug,
			Name:     l.Name,
			Severity: l.Severity,
			Body:     l.Body,
			Scope:    l.Scope,
		})
	}
	return func() []agent.LawInfo {
		out := make([]agent.LawInfo, len(snapshot))
		copy(out, snapshot)
		return out
	}
}

func personaCatalog(bundle config.Bundle) agent.PersonaCatalog {
	snapshot := make([]agent.PersonaInfo, 0, len(bundle.Personas))
	for _, p := range bundle.Personas {
		snapshot = append(snapshot, agent.PersonaInfo{
			Slug:        p.Slug,
			Name:        p.Name,
			Description: p.Description,
			Body:        p.Body,
			Skills:      append([]string(nil), p.Skills...),
			Laws:        append([]string(nil), p.Laws...),
		})
	}
	return func() []agent.PersonaInfo {
		out := make([]agent.PersonaInfo, len(snapshot))
		copy(out, snapshot)
		return out
	}
}

func commandCatalog(bundle config.Bundle) agent.CommandCatalog {
	snapshot := make(map[string]agent.MCPCommandBinding, len(bundle.MCPCommands))
	for name, spec := range bundle.MCPCommands {
		snapshot[name] = agent.MCPCommandBinding{
			Persona:      spec.Persona,
			Laws:         append([]string(nil), spec.Laws...),
			LawsDisabled: append([]string(nil), spec.LawsDisabled...),
			Templates:    append([]string(nil), spec.Templates...),
		}
	}
	return func() map[string]agent.MCPCommandBinding {
		out := make(map[string]agent.MCPCommandBinding, len(snapshot))
		for k, v := range snapshot {
			out[k] = agent.MCPCommandBinding{
				Persona:      v.Persona,
				Laws:         append([]string(nil), v.Laws...),
				LawsDisabled: append([]string(nil), v.LawsDisabled...),
				Templates:    append([]string(nil), v.Templates...),
			}
		}
		return out
	}
}

func summarizeTaskTemplate(t *config.TaskTemplate) *agent.TaskTemplateSummary {
	if t == nil {
		return nil
	}
	return &agent.TaskTemplateSummary{
		Slug:        t.Slug,
		Name:        t.Name,
		Description: t.Description,
		Body:        t.Body,
	}
}

func resolvedConfigPath(path string) (string, error) {
	if path != "" {
		return filepath.Abs(path)
	}
	return paths.ConfigFile()
}

func resolvedDBPath(path string) (string, error) {
	if path != "" {
		return filepath.Abs(path)
	}
	return paths.DatabaseFile()
}
