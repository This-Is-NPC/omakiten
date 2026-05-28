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
	"os"
	"path/filepath"

	"omakiten/internal/agent"
	"omakiten/internal/config"
	"omakiten/internal/configstore"
	"omakiten/internal/events"
	"omakiten/internal/hooks"
	"omakiten/internal/hooks/actions"
	"omakiten/internal/paths"
	project_ "omakiten/internal/project"
	"omakiten/internal/sqlite"
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
// dispatch through. Phase 3a hoisted the per-bundle resources
// (service, hooks engine, registry, notification snapshot) into the
// BundleCache; Runtime keeps thin accessors so consumers do not need
// to know whether the cache returned an existing entry or built a new
// one.
type Runtime struct {
	store      *sqlite.Store
	configPath string
	dbPath     string
	bus        events.Bus
	cache      *BundleCache
	// defaultProjectID is the cache key the boot path installed the
	// initial runtime under. Phase 3a always uses 0 (single bundle
	// process-wide); Phase 3b–3f switch to per-project ids without
	// touching the rest of this file.
	defaultProjectID int64
	// actionRegistry is the same registry the active runtime's engine
	// reads from. Held on Runtime so external callers (tests, future
	// MCP plugins) can extend the registry before a reload picks it up.
	actionRegistry     *hooks.ActionRegistry
	notificationAction *actions.NotificationShowAction
}

// Open materializes the runtime: resolves paths, runs config layout
// migration + default-file seeding, opens the sqlite store, imports the
// bundle, and wires the agent.Service with template snapshots.
func Open(ctx context.Context, opts Options) (*Runtime, error) {
	dbPath, err := resolvedDBPath(opts.DBPath)
	if err != nil {
		return nil, err
	}

	cs := configstore.New()
	rootDir, err := resolvedConfigRoot(opts.ConfigPath)
	if err != nil {
		return nil, err
	}
	if err := cs.MigrateLayout(rootDir); err != nil {
		return nil, err
	}
	if err := cs.EnsureDefaultFiles(rootDir); err != nil {
		return nil, err
	}

	// Resolve configPath AFTER MigrateLayout has had a chance to relocate
	// renamed kits — otherwise the snapshot points at a just-moved root
	// copy and Import fails with ENOENT.
	configPath, err := resolvedConfigPath(opts.ConfigPath)
	if err != nil {
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

	// Peek the bundle once to construct the bus. The bus depends on
	// the events policy and must outlive every cache rebuild — putting
	// it inside the BundleCache would force every Reload to reseat
	// subscribers (TUI panels, hooks engine). Cache rebuilds keep
	// using this same bus handle.
	preview, err := config.LoadBundle(configPath)
	if err != nil {
		_ = store.Close()
		return nil, err
	}

	// Hydrate the domain event_type registry from the kit YAML
	// before the bundle cache resolves services that consume it
	// (formatter resolution, log-visibility gating, metric routing).
	// No-op when the events block has no definitions so fixture-only
	// runtimes stay unaffected.
	if err := config.LoadDomainEventRegistry(preview.Config.Events); err != nil {
		_ = store.Close()
		return nil, err
	}

	bus := events.NewInProcessBus(preview.Config.Events)

	cwd := opts.CWD
	if cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			_ = store.Close()
			return nil, err
		}
	}
	// Cache owns the selector so every rebuild (mtime change, explicit
	// Reload) constructs services that retain the boot-resolved
	// project / CWD. SetProjectSelector must precede Resolve so the
	// initial build picks it up.
	cache := NewBundleCache(store, bus, cs)
	cache.SetProjectSelector(agent.ProjectSelector{ProjectID: opts.ProjectID, Project: opts.Project, CWD: cwd})
	rt, err := cache.Resolve(ctx, opts.ProjectID, configPath)
	if err != nil {
		_ = store.Close()
		return nil, err
	}

	r := &Runtime{
		store:              store,
		configPath:         configPath,
		dbPath:             dbPath,
		bus:                bus,
		cache:              cache,
		defaultProjectID:   opts.ProjectID,
		actionRegistry:     rt.ActionRegistry,
		notificationAction: rt.NotificationAction,
	}
	return r, nil
}

func (r *Runtime) Close() error {
	if pr := r.cache.Get(r.defaultProjectID); pr != nil && pr.HooksEngine != nil {
		pr.HooksEngine.Stop()
	}
	return r.store.Close()
}

// Cache exposes the BundleCache so consumers (TUI hot-reload, future
// per-project surfaces) can call Resolve / Reload without going
// through Runtime first.
func (r *Runtime) Cache() *BundleCache {
	return r.cache
}

// Snapshot returns the active *config.Snapshot for the runtime's
// default project. Returns nil when the cache has not yet built a
// runtime for the default project (rare bootstrap window). Callers
// that need a project-specific snapshot route through Cache().Get(id).
func (r *Runtime) Snapshot() *config.Snapshot {
	if pr := r.cache.Get(r.defaultProjectID); pr != nil {
		return pr.Snapshot
	}
	return nil
}

// ResolveServiceForProject returns the agent.Service the BundleCache
// has wired for the given project. The lookup is best-effort: when
// the project slug / id resolve to an entry without a per-project
// `.omakiten/` install, or when any step in the resolution chain
// fails, the function returns (nil, nil) so callers can fall back to
// the default service without surfacing the discrepancy to the agent
// caller.
//
// Phase 3b uses this from the MCP adapter to route each tool call to
// the project the caller declared in `project` / `project_id`. Phase
// 3c+ will extend the same routing to CLI and TUI without touching
// this method's surface.
func (r *Runtime) ResolveServiceForProject(ctx context.Context, project string, projectID int64) (*agent.Service, error) {
	if project == "" && projectID == 0 {
		return nil, nil
	}
	resolved, err := project_.NewResolver(r.store).Resolve(ctx, project_.ResolveOptions{ProjectID: projectID, Project: project})
	if err != nil || resolved.RootPath == "" {
		return nil, nil
	}
	repoLocal, ok, err := config.FindRepoLocal(resolved.RootPath)
	if err != nil || !ok {
		// Project has no per-project install — the default runtime
		// already serves the right bundle (single-bundle process-wide).
		return nil, nil
	}
	configFile, err := paths.ActiveConfigFileInDir(filepath.Join(repoLocal, "config"))
	if err != nil || configFile == "" {
		return nil, nil
	}
	pr, err := r.cache.Resolve(ctx, resolved.ID, configFile)
	if err != nil || pr == nil {
		return nil, nil
	}
	return pr.Service, nil
}

// buildHookEntries lifts user-facing HookSpec entries into the
// engine's hooks.Hook shape. Notification-shape entries are rewritten to
// call notification.show; per-hook message overrides ride along under
// dedicated arg keys so the action can use them when the notification YAML
// has no message source of its own.
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

func buildDepthAwareHookEntries(snapshot *config.Snapshot) []hooks.Hook {
	if snapshot == nil {
		return nil
	}
	entries := buildHookEntries(snapshot.Hooks())
	rootDepth := hooks.SubjectDepthAny
	if _, ok := snapshot.SubtaskKit(); ok {
		rootDepth = hooks.SubjectDepthRoot
	}
	rootKey := snapshot.Kit().Key
	for i := range entries {
		entries[i].SubjectDepth = rootDepth
		entries[i].ResolvedKit = rootKey
		stampNotificationResolvedKit(&entries[i], rootKey)
	}
	if sub, ok := snapshot.SubtaskKit(); ok {
		subEntries := buildHookEntries(sub.Hooks())
		subKey := sub.Kit().Key
		for i := range subEntries {
			subEntries[i].SubjectDepth = hooks.SubjectDepthSubtask
			subEntries[i].ResolvedKit = subKey
			stampNotificationResolvedKit(&subEntries[i], subKey)
		}
		entries = append(entries, subEntries...)
	}
	return entries
}

// stampNotificationResolvedKit threads the hook entry's resolved kit
// identity into the notification.show action args so the action picks
// the right per-kit catalog at dispatch time (#301 review §11557
// finding A5). No-op for non-notification entries.
func stampNotificationResolvedKit(hook *hooks.Hook, kitKey string) {
	if hook.Do != actions.NotificationActionName {
		return
	}
	if kitKey == "" {
		return
	}
	if hook.Args == nil {
		hook.Args = map[string]any{}
	}
	hook.Args[actions.NotificationArgResolvedKit] = kitKey
}

// ActionRegistry exposes the hook action registry so callers (TUI startup,
// tests) can register additional actions before the engine is busy.
func (r *Runtime) ActionRegistry() *hooks.ActionRegistry {
	return r.actionRegistry
}

// Bus returns the in-process events bus. Subscribers (live TUI panels,
// future notifications) register via this handle.
func (r *Runtime) Bus() events.Bus {
	return r.bus
}

func (r *Runtime) Service() *agent.Service {
	if pr := r.cache.Get(r.defaultProjectID); pr != nil {
		return pr.Service
	}
	return nil
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

func resolvedConfigPath(path string) (string, error) {
	if path != "" {
		return filepath.Abs(path)
	}
	return paths.ConfigFile()
}

// resolvedConfigRoot mirrors the CLI helper of the same intent: compute the
// migration root without consulting ActiveConfigFile, so MigrateLayout can
// run before path resolution. When the agent runtime is invoked with an
// explicit ConfigPath, root is derived from that path; otherwise from XDG /
// OMAKITEN_HOME defaults.
func resolvedConfigRoot(path string) (string, error) {
	if path != "" {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		return config.ConfigRootFromYAMLPath(abs), nil
	}
	return paths.ConfigRoot()
}

func resolvedDBPath(path string) (string, error) {
	if path != "" {
		return filepath.Abs(path)
	}
	return paths.DatabaseFile()
}
