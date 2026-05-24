package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"omakiten/internal/agent"
	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/configstore"
	"omakiten/internal/domain"
	"omakiten/internal/events"
	"omakiten/internal/hooks"
	"omakiten/internal/hooks/actions"
	"omakiten/internal/sqlite"
)

// ProjectRuntime aggregates every per-bundle resource derived from a
// single ConfigService.Import call. One instance per project lives in
// the BundleCache; today the cache holds exactly one (the default
// project), but the type and the cache are shaped so Phase 3b–3f can
// add per-project entries without touching consumer code.
//
// The fields are documented from the consumer's perspective: callers
// that need to read config go through Snapshot (immutable, per-project);
// callers that need to dispatch MCP/CLI calls take Service; the hooks
// engine, the audit registry, and the notification snapshot are owned
// per runtime so a reload can stop the old engine cleanly before the
// new one starts. The raw config.Bundle is intentionally not exposed —
// Phase 2-bis Invariant 1 keeps every consumer reading through Snapshot
// so the Store/Bundle reverse-coupling cannot creep back in.
type ProjectRuntime struct {
	// Service is the agent service wired against the bundle's
	// catalogs, lookups, and settings. Stateless aside from the
	// snapshots it captures at construction.
	Service *agent.Service
	// HooksEngine is the running engine subscribed to the bus. Stopped
	// on Reload before the new engine starts.
	HooksEngine *hooks.Engine
	// ActionRegistry is the hooks action registry the engine resolves
	// `do:` names against. Held on the runtime so external callers
	// (TUI start-up, tests) can register additional actions before a
	// reload picks them up.
	ActionRegistry *hooks.ActionRegistry
	// NotificationAction is the notification.show action registered
	// against the snapshot above. Held for the same reason the
	// registry is — callers occasionally need to push a notification
	// outside the hooks bus.
	NotificationAction *actions.NotificationShowAction
	// EnumRegistry resolves priority and severity id↔value pairs from
	// the bundle's enum tables. Threaded into the agent service so
	// renderers do not consult process-global state.
	EnumRegistry *domain.EnumRegistry
	// NotificationSnapshot is the catalog the notification.show action
	// reads from. Owned by the runtime so a reload can rotate it.
	NotificationSnapshot actions.NotificationBundleSnapshot
	// Theme is the active theme resolved at load time. nil when no
	// theme is configured — TUI surfaces fall back to their default
	// palette.
	Theme *config.Theme
	// SourcePath is the absolute path to the omakiten.yaml that
	// produced this runtime. Used by Reload to stat-detect bundle
	// changes.
	SourcePath string
	// LoadedAt is the wall-clock timestamp the runtime finished
	// initialising. Used by /metrics.summary timelines and the TUI
	// "config loaded at" badge.
	LoadedAt time.Time
	// Mtime is the SourcePath's modification time captured at load. A
	// stat comparison in Resolve drives the rebuild-on-change rule.
	Mtime time.Time
	// Snapshot is the immutable per-project view of the loaded bundle.
	// Every app service consumed by this runtime reads workflow shape,
	// catalogs, settings, hooks, events and synonyms through this
	// pointer; the cache's swap on rebuild installs a new pointer and
	// in-flight readers keep the previous one until they return.
	Snapshot *config.Snapshot
	// PreviousSnapshot is the snapshot the runtime carried immediately
	// before the latest build. Captured into the new entry whenever the
	// cache rotates so the orphan-detection flow can resolve
	// task.bucket_id → previous key across the rebuild. nil when the
	// runtime has only been built once for this project.
	PreviousSnapshot *config.Snapshot
	// Workflow is the per-project app.WorkflowService captured against
	// this runtime's Snapshot. TUI / CLI surfaces that hold a long-lived
	// workflow reference (e.g. tui.Repositories.Workflow) read this
	// pointer on Reload so the rotation rebuilds the service rather than
	// mutating it through a setter — the immutability invariant the
	// Phase 2-bis Round-2 spec requires.
	Workflow *app.WorkflowService
}

// BundleCache is the per-project ProjectRuntime registry. Phase 3a
// keeps the cache size at 1 (the default project) — the type is shaped
// for the multi-project future where each project's bundle lives in
// its own entry. Reads take RLock; rebuilds take Lock; the swap is
// pointer-only so concurrent Resolves on other project ids never block
// a rebuild in progress for a different id.
type BundleCache struct {
	mu      sync.RWMutex
	entries map[int64]*ProjectRuntime

	// Dependencies the cache needs to build a runtime. Stored on the
	// cache so Resolve does not require the caller to thread them in.
	store *sqlite.Store
	bus   events.Bus
	// configstore wires the in-memory bundle editor + saver. Shared
	// across projects: the store knows the per-project root from
	// SourcePath, so the editor is set per-runtime, not per-cache.
	cs *configstore.Adapter

	// selectorMu guards selector — SetProjectSelector may be called
	// from the boot path while another goroutine triggers rebuild.
	selectorMu sync.RWMutex
	// selector is threaded into every Service the cache builds so a
	// mtime-driven rebuild does not lose the boot-resolved
	// project/CWD. Zero value when no selector was installed (rare
	// boot shapes that resolve project per call).
	selector agent.ProjectSelector
}

// NewBundleCache constructs an empty cache. Open seeds the first entry
// after the default-project bundle is built.
func NewBundleCache(store *sqlite.Store, bus events.Bus, cs *configstore.Adapter) *BundleCache {
	return &BundleCache{
		entries: map[int64]*ProjectRuntime{},
		store:   store,
		bus:     bus,
		cs:      cs,
	}
}

// SetProjectSelector installs the project selector every subsequent
// build (Resolve on miss, Reload, rebuild on mtime change) applies to
// the constructed agent.Service. Without this, a mtime-triggered
// rebuild rotates to a service with an empty selector and calls that
// rely on the boot-resolved project / CWD silently lose context. The
// composition root calls this once after Open resolves the runtime
// project; tests that drive the cache directly may leave it unset and
// build services with a zero selector.
func (c *BundleCache) SetProjectSelector(selector agent.ProjectSelector) {
	c.selectorMu.Lock()
	c.selector = selector
	c.selectorMu.Unlock()
}

func (c *BundleCache) projectSelector() agent.ProjectSelector {
	c.selectorMu.RLock()
	defer c.selectorMu.RUnlock()
	return c.selector
}

// Get returns the cached runtime for projectID without consulting the
// filesystem. Returns nil when the entry has not been built yet.
func (c *BundleCache) Get(projectID int64) *ProjectRuntime {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.entries[projectID]
}

// Resolve returns the runtime for projectID, building one on cache
// miss or rebuilding when the source bundle's mtime has changed. The
// configPath argument is honoured only on the first build for a given
// id (a cached entry remembers its own SourcePath); subsequent calls
// pass "" to skip the path argument when they only want the cached
// pointer. The projectID flows into the constructed engine so its
// dispatch filter accepts only events scoped to this project (Phase
// 3d).
func (c *BundleCache) Resolve(ctx context.Context, projectID int64, configPath string) (*ProjectRuntime, error) {
	c.mu.RLock()
	entry := c.entries[projectID]
	c.mu.RUnlock()

	if entry != nil {
		path := entry.SourcePath
		if path == "" {
			path = configPath
		}
		if path == "" {
			return entry, nil
		}
		// Stat the source bundle. When the file is missing (rare —
		// user moved or renamed it after boot) we keep serving the
		// cached entry rather than crashing; a manual Reload surfaces
		// the failure to the caller.
		info, err := os.Stat(path)
		if err != nil {
			return entry, nil
		}
		if info.ModTime().Equal(entry.Mtime) {
			return entry, nil
		}
		// Mtime changed — rebuild against the resolved path. Callers
		// that passed configPath="" still get the correct rebuild
		// because we route the cached SourcePath through.
		return c.rebuild(ctx, projectID, path)
	}

	return c.rebuild(ctx, projectID, configPath)
}

// Reload forces a rebuild of the runtime for projectID regardless of
// the cached entry's mtime. Used by the TUI hot-reload path and by
// tests that need to confirm the rebuild stops the previous engine
// before the new one starts.
func (c *BundleCache) Reload(ctx context.Context, projectID int64, configPath string) (*ProjectRuntime, error) {
	return c.rebuild(ctx, projectID, configPath)
}

// Install seeds the cache with a runtime that was built outside the
// cache (e.g. by Open during boot). The Mtime is captured here so the
// next Resolve can stat-detect changes against the right baseline.
//
// Engine.Stop runs after the swap and OUTSIDE the cache mutex —
// mirrors rebuild's pattern so a slow drain (wg.Wait in Stop) cannot
// deadlock concurrent Resolves.
func (c *BundleCache) Install(projectID int64, runtime *ProjectRuntime) {
	c.mu.Lock()
	old := c.entries[projectID]
	c.entries[projectID] = runtime
	c.mu.Unlock()

	if old != nil && old.HooksEngine != nil && old != runtime {
		old.HooksEngine.Stop()
	}
}

// Size reports the number of cached entries. Exposed primarily for
// tests that want to assert the Phase 3a invariant (cache size = 1).
func (c *BundleCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// rebuild parses the bundle at configPath, builds a fresh
// ProjectRuntime, swaps the cache entry, and stops the previous
// runtime's hooks engine. The lock is held only across the swap to
// keep concurrent Resolves on other ids unblocked.
func (c *BundleCache) rebuild(ctx context.Context, projectID int64, configPath string) (*ProjectRuntime, error) {
	if c.store == nil {
		return nil, fmt.Errorf("bundle cache: store is required")
	}
	if c.cs == nil {
		return nil, fmt.Errorf("bundle cache: configstore is required")
	}
	if configPath == "" {
		return nil, fmt.Errorf("bundle cache: configPath is required for project %d", projectID)
	}

	runtime, err := buildProjectRuntime(ctx, c.store, c.cs, c.bus, configPath, projectID, c.projectSelector())
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	old := c.entries[projectID]
	// Carry the prior snapshot forward so the orphan flow can resolve
	// task.bucket_id → previous key across the rebuild. Skip the
	// carry on the very first build (old == nil) — there is no
	// useful id↔key mapping to project from a non-existent entry.
	if old != nil {
		runtime.PreviousSnapshot = old.Snapshot
		// Re-inject the orphan service with both snapshot pointers
		// in hand. buildProjectRuntime injected one with prev=nil
		// because the prior entry is only visible inside the cache;
		// the rotation overwrites it with the rebind-capable view.
		orphan := app.NewOrphanService(c.store, runtime.Snapshot, runtime.PreviousSnapshot)
		// Wire the post-migrate consumer: once the rebind round runs,
		// drop the cached PreviousSnapshot pointer so the prior bundle
		// can be GC'd. Without this, a long-running TUI session that
		// triggered one orphan flow held the prior Snapshot in RAM
		// forever even though no further reader needed it (task #228).
		orphan.SetMigrateConsumer(func() { c.releasePreviousSnapshot(projectID) })
		runtime.Service.SetOrphanService(orphan)
	}
	c.entries[projectID] = runtime
	c.mu.Unlock()

	if old != nil && old.HooksEngine != nil {
		old.HooksEngine.Stop()
	}
	return runtime, nil
}

// releasePreviousSnapshot drops the PreviousSnapshot pointer on the
// project's runtime entry so the prior bundle's Snapshot tree
// (workflows, entities, locale packs) can be GC'd. Called by the
// OrphanService's onMigrate consumer after a successful rebind —
// nobody else holds a reference at that point, and keeping it alive
// just pins the prior bundle in RAM indefinitely. No-op when the
// entry has already rotated again (rare race) or when PreviousSnapshot
// is already nil.
//
// Lock order: acquires c.mu (write side). Callers MUST NOT hold any
// other lock that c.mu's writers can block on. Today that set is
// empty — OrphanService.Migrate runs the onMigrate consumer from a
// dedicated goroutine that holds no caller-side locks. If a future
// refactor introduces a lock that BundleCache.rebuild + this helper
// could both transitively acquire, defer the release via tea.Cmd /
// goroutine so the consumer never lock-couples with the caller.
func (c *BundleCache) releasePreviousSnapshot(projectID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[projectID]
	if !ok || entry == nil {
		return
	}
	entry.PreviousSnapshot = nil
}

// BuildProjectRuntime is the single point of bundle inflation. Open
// uses it for the boot path; BundleCache.rebuild calls it on
// stat-detected changes and explicit reloads; the CLI composition
// root reuses it via cache.Resolve so a single construction path
// produces identical runtimes everywhere — drift between boot and
// reload was the bug that motivated the Phase 3a refactor.
//
// selector flows into the constructed agent.Service so calls without
// explicit project arguments still see the boot-resolved project /
// CWD; pass a zero value when callers always provide selectors per
// call.
func BuildProjectRuntime(ctx context.Context, store *sqlite.Store, cs *configstore.Adapter, bus events.Bus, configPath string, projectID int64, selector agent.ProjectSelector) (*ProjectRuntime, error) {
	return buildProjectRuntime(ctx, store, cs, bus, configPath, projectID, selector)
}

func buildProjectRuntime(ctx context.Context, store *sqlite.Store, cs *configstore.Adapter, bus events.Bus, configPath string, projectID int64, selector agent.ProjectSelector) (*ProjectRuntime, error) {
	bundle, bundleHash, enumRegistry, err := app.NewConfigService(cs).Import(ctx, configPath)
	if err != nil {
		return nil, err
	}
	// Emit the bundle.imported audit event so hooks subscribed to
	// configuration content changes continue to fire after Round-2
	// retired Store.ImportBundle. Payload is composed here at the
	// composition root because the SQL adapter must remain free of
	// config.Bundle references (Phase 2-bis gate 2). Failure to
	// record the audit row is non-fatal — the snapshot still loads
	// and the runtime still boots — so callers proceed even when
	// telemetry gating drops the event.
	workflowKey := bundle.Config.Workflow.Active
	if workflowKey == "" && len(bundle.Workflows) > 0 {
		workflowKey = bundle.Workflows[0].Key
	}
	auditPayload, _ := json.Marshal(map[string]any{
		"path":           configPath,
		"hash":           bundleHash,
		"workflow_key":   workflowKey,
		"workflow_count": len(bundle.Workflows),
		"persona_count":  len(bundle.Personas),
		"skill_count":    len(bundle.Skills),
		"law_count":      len(bundle.Laws),
		"template_count": len(bundle.Templates),
	})
	_ = store.RecordEntityEvent(ctx, domain.EventEntitySystem, 0, 0, domain.EventTypeBundleImported, string(auditPayload))

	// Build the per-project Snapshot up front so every downstream wire —
	// notification catalog, hooks engine, agent service — reads from the
	// same immutable pointer. Two projects in the cache hold two
	// snapshots; nothing in the hot path reaches back into the bundle.
	snapshot := config.BuildSnapshot(bundle)

	notifSvc := app.NewNotificationService(snapshot)
	notifSnapshot := notifSvc.BundleSnapshot()
	// The CLI surface catalog handles notification chrome expansion
	// (${{intl:KEY}} tokens). Set here at the composition root because the
	// app layer is constrained by the i18n arch boundary from naming
	// config.SurfaceCLI directly.
	notifSnapshot.Catalog = snapshot.Catalog(config.SurfaceCLI)
	registry := hooks.NewActionRegistry()
	actions.RegisterBuiltins(registry)
	notificationAction := actions.NewNotificationShowAction(notifSnapshot)
	registry.Register(notificationAction)

	if err := config.ValidateHooks(bundle.Config.Hooks, func(name string) bool {
		_, ok := registry.Get(name)
		return ok
	}, snapshot.Notifications()); err != nil {
		return nil, err
	}

	if err := store.ApplyConfig(ctx, sqlite.ConfigKnobs{
		BusyTimeoutMs:            bundle.Config.SQLite.BusyTimeoutMs,
		CacheSizeKB:              bundle.Config.SQLite.CacheSizeKB,
		MmapSizeBytes:            bundle.Config.SQLite.MmapSizeBytes,
		ActivityLogMaxRows:       bundle.Config.ActivityLog.MaxRows,
		ActivityLogMaxAgeDays:    bundle.Config.ActivityLog.MaxAgeDays,
		EventsDefaultRecentLimit: bundle.Config.Events.DefaultRecentLimit,
		EventsPolicy:             bundle.Config.Events,
		EventBus:                 bus,
	}); err != nil {
		return nil, err
	}
	hookEntries := buildHookEntries(snapshot.Hooks())
	engine := hooks.NewEngine(hookEntries, registry, snapshot.Events(), store)
	engine.SetProjectID(projectID)
	if bus != nil {
		engine.Start(bus)
	}

	// snapshot is built above before the hooks engine — both surfaces
	// consume the same immutable per-project pointer. Each rebuild
	// produces a fresh pointer; in-flight callers that captured the
	// previous pointer continue reading from it until they return.

	svc := agent.NewService(store, selector)
	// SetSnapshot is the single wiring entry point: the agent service
	// derives the catalog closures, synonym table, stopword set, and
	// bundle-scoped EnumRegistry from the per-project Snapshot in one
	// pass. Two projects holding two snapshots see two independent
	// catalog views; hot-reload rotates the pointer atomically through
	// cache.Reload.
	svc.SetSnapshot(snapshot)
	// Inject the orphan service with prev=nil — the cache rotation
	// overrides this with a rebind-capable view (current+previous
	// snapshots) when the runtime is replacing an earlier entry.
	svc.SetOrphanService(app.NewOrphanService(store, snapshot, nil))
	// Best-effort backfill: populate tasks.completed_at for historical
	// done-bucket rows missing the timestamp. Idempotent — subsequent
	// builds find no rows to update. Errors are swallowed so a hot
	// transient (FK lock, etc.) cannot block runtime composition.
	_, _ = store.BackfillTaskCompletedAt(ctx, projectID, snapshot)
	svc.SetSettings(agent.ServiceSettings{
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

	mtime := time.Time{}
	if info, err := os.Stat(configPath); err == nil {
		mtime = info.ModTime()
	}

	return &ProjectRuntime{
		Service:              svc,
		HooksEngine:          engine,
		ActionRegistry:       registry,
		NotificationAction:   notificationAction,
		EnumRegistry:         enumRegistry,
		NotificationSnapshot: notifSnapshot,
		SourcePath:           configPath,
		LoadedAt:             time.Now(),
		Mtime:                mtime,
		Snapshot:             snapshot,
		Workflow:             app.NewWorkflowServiceFromStore(store, snapshot.Registry(), snapshot),
		// PreviousSnapshot is populated by the cache on rotation —
		// buildProjectRuntime has no access to the prior entry.
	}, nil
}

