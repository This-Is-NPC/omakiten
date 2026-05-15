package agentruntime

import (
	"context"
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
// that need to read the bundle take Bundle; callers that need to
// dispatch MCP/CLI calls take Service; the hooks engine, the audit
// registry, and the notification snapshot are owned per runtime so a
// reload can stop the old engine cleanly before the new one starts.
type ProjectRuntime struct {
	// Bundle is the resolved config snapshot. Pointer so the cache can
	// hand the same value to several readers without copying the
	// ~30-field struct.
	Bundle *config.Bundle
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
	// TagSynonyms is the alias table the app's tag normaliser threads
	// into NormalizeTagName via per-service SetSynonyms (Phase 3f
	// dropped the process-global registry).
	TagSynonyms map[string]string
	// Stopwords is the similar-task ranker's stopword list, threaded
	// into agent.Service via SetStopwords (Phase 3f).
	Stopwords []string
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
func (c *BundleCache) Install(projectID int64, runtime *ProjectRuntime) {
	c.mu.Lock()
	defer c.mu.Unlock()
	old, hadOld := c.entries[projectID]
	c.entries[projectID] = runtime
	if hadOld && old != nil && old.HooksEngine != nil {
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

	runtime, err := buildProjectRuntime(ctx, c.store, c.cs, c.bus, configPath, projectID)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	old := c.entries[projectID]
	c.entries[projectID] = runtime
	c.mu.Unlock()

	if old != nil && old.HooksEngine != nil {
		old.HooksEngine.Stop()
	}
	return runtime, nil
}

// buildProjectRuntime is the single point of bundle inflation. Open
// uses it for the boot path; BundleCache.rebuild calls it on
// stat-detected changes and explicit reloads. Keeping the
// construction here means both code paths produce identical runtimes
// — diff drift between boot and reload was the bug that motivated
// the Phase 3a refactor.
//
// applyStoreKnobs controls whether the freshly-parsed bundle's runtime
// knobs (busy_timeout, activity-log retention, events policy) are
// pushed into the Store. Boot passes true; reloads pass true too —
// the user may have changed any of them in YAML and Phase 3a is
// supposed to land plumbing without altering observable behaviour.
func buildProjectRuntime(ctx context.Context, store *sqlite.Store, cs *configstore.Adapter, bus events.Bus, configPath string, projectID int64) (*ProjectRuntime, error) {
	bundle, _, enumRegistry, err := app.NewConfigService(store, cs).Import(ctx, configPath)
	if err != nil {
		return nil, err
	}

	notifSnapshot := actions.NotificationBundleSnapshot{Notifications: bundle.Notifications}
	registry := hooks.NewActionRegistry()
	actions.RegisterBuiltins(registry)
	notificationAction := actions.NewNotificationShowAction(notifSnapshot)
	registry.Register(notificationAction)

	if err := config.ValidateHooks(bundle.Config.Hooks, func(name string) bool {
		_, ok := registry.Get(name)
		return ok
	}, bundle.Notifications); err != nil {
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
		return nil, err
	}

	hookEntries := buildHookEntries(bundle.Config.Hooks)
	engine := hooks.NewEngine(hookEntries, registry, bundle.Config.Events, store)
	engine.SetProjectID(projectID)
	if bus != nil {
		engine.Start(bus)
	}

	svc := agent.NewService(store, agent.ProjectSelector{})
	svc.SetRegistry(enumRegistry)
	svc.SetTaskTemplateLookup(taskTemplateLookup(bundle))
	svc.SetTemplateCatalog(templateCatalog(bundle))
	svc.SetSkillCatalog(skillCatalog(bundle))
	svc.SetLawCatalog(lawCatalog(bundle))
	svc.SetPersonaCatalog(personaCatalog(bundle))
	svc.SetCommandCatalog(commandCatalog(bundle))
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
	// Phase 3f: per-project synonyms + stopwords on the agent.Service.
	// Service builder helpers (newCommentService / newTagService / ...)
	// forward s.synonyms into NormalizeTagName so two projects' tables
	// stay disjoint.
	svc.SetSynonyms(copyStringMap(bundle.Config.TagSynonyms))
	svc.SetStopwords(append([]string(nil), bundle.Config.Search.Stopwords...))

	mtime := time.Time{}
	if info, err := os.Stat(configPath); err == nil {
		mtime = info.ModTime()
	}

	return &ProjectRuntime{
		Bundle:               &bundle,
		Service:              svc,
		HooksEngine:          engine,
		ActionRegistry:       registry,
		NotificationAction:   notificationAction,
		EnumRegistry:         enumRegistry,
		NotificationSnapshot: notifSnapshot,
		TagSynonyms:          copyStringMap(bundle.Config.TagSynonyms),
		Stopwords:            append([]string(nil), bundle.Config.Search.Stopwords...),
		SourcePath:           configPath,
		LoadedAt:             time.Now(),
		Mtime:                mtime,
	}, nil
}

func copyStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
