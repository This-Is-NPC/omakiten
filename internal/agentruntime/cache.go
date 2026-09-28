package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"omakiten/internal/activity"
	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/configstore"
	"omakiten/internal/domain"
	"omakiten/internal/events"
	"omakiten/internal/hooks"
	"omakiten/internal/hooks/actions"
	"omakiten/internal/operation"
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
	Service *operation.Service
	// HooksEngine is the runtime-owned engine. BundleCache starts it only
	// after the previous engine has quiesced successfully.
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
	// SourcePaths includes SourcePath plus any loaded sub-kit files that
	// should trigger the same rebuild when their mtime changes.
	SourcePaths []string
	// LoadedAt is the wall-clock timestamp the runtime finished
	// initialising. Used by /metrics.summary timelines and the TUI
	// "config loaded at" badge.
	LoadedAt time.Time
	// Mtime is the SourcePath's modification time captured at load. A
	// stat comparison in Resolve drives the rebuild-on-change rule.
	Mtime time.Time
	// SourceMtimes records the mtime for every watched source path.
	SourceMtimes map[string]time.Time
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
	// Editor is the bundle editor wired against this runtime's config
	// path. The TUI host copies it onto Repositories.Editor so it does
	// not construct app.BundleEditor itself (D20).
	Editor *app.BundleEditor
	// BundleImportedPayload is committed by the cache only after a consumer
	// accepts the runtime. Keeping it on the inactive runtime prevents a
	// failed consumer rebind from leaving an audit row for a runtime that was
	// never made live.
	BundleImportedPayload string
	// StoreConfig is prepared with the runtime and committed only after the
	// consumer accepts the candidate. Keeping it here prevents bundle parsing
	// from changing the live Store or event bus.
	StoreConfig sqlite.ConfigKnobs
}

// BundleCache is the per-project ProjectRuntime registry. Phase 3a
// keeps the cache size at 1 (the default project) — the type is shaped
// for the multi-project future where each project's bundle lives in
// its own entry. Reads take RLock; lifecycle rebuilds are serialized so
// old and replacement hook engines cannot overlap; publication is a
// pointer-only swap after the old engine drains.
type BundleCache struct {
	mu        sync.RWMutex
	entries   map[int64]*ProjectRuntime
	rebuildMu sync.Mutex

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
	selector operation.ProjectSelector

	notifyMu          sync.RWMutex
	onSurfacesChanged func()
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
// the constructed operation.Service. Without this, a mtime-triggered
// rebuild rotates to a service with an empty selector and calls that
// rely on the boot-resolved project / CWD silently lose context. The
// composition root calls this once after Open resolves the runtime
// project; tests that drive the cache directly may leave it unset and
// build services with a zero selector.
func (c *BundleCache) SetProjectSelector(selector operation.ProjectSelector) {
	c.selectorMu.Lock()
	c.selector = selector
	c.selectorMu.Unlock()
}

func (c *BundleCache) projectSelector() operation.ProjectSelector {
	c.selectorMu.RLock()
	defer c.selectorMu.RUnlock()
	return c.selector
}

// SetSurfacesChangedNotify installs a hook fired after a successful
// rebuild when the surfaces: fingerprint changes. The callback must
// not block — Reload returns as soon as it returns. Typical wiring is
// a non-blocking send on a buffered channel that mcp.ServeNotify reads.
func (c *BundleCache) SetSurfacesChangedNotify(fn func()) {
	c.notifyMu.Lock()
	c.onSurfacesChanged = fn
	c.notifyMu.Unlock()
}

func (c *BundleCache) fireSurfacesChanged() {
	c.notifyMu.RLock()
	fn := c.onSurfacesChanged
	c.notifyMu.RUnlock()
	if fn != nil {
		fn()
	}
}

func (c *BundleCache) maybeNotifySurfacesChanged(old, next *ProjectRuntime) {
	if old == nil || next == nil {
		return
	}
	if surfacesFingerprint(old.Snapshot) == surfacesFingerprint(next.Snapshot) {
		return
	}
	c.fireSurfacesChanged()
}

func surfacesFingerprint(snap *config.Snapshot) string {
	if snap == nil {
		return ""
	}
	table := snap.Surfaces()
	if len(table) == 0 {
		return ""
	}
	slugs := make([]string, 0, len(table))
	for slug := range table {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	var b strings.Builder
	for _, slug := range slugs {
		row := table[slug]
		b.WriteString(slug)
		b.WriteByte('=')
		writeSurfaceBit(&b, row.CLI)
		writeSurfaceBit(&b, row.TUI)
		writeSurfaceBit(&b, row.MCP)
		b.WriteByte('|')
		b.WriteString(row.Reason)
		b.WriteByte(';')
	}
	return b.String()
}

func writeSurfaceBit(b *strings.Builder, p *bool) {
	if p == nil {
		b.WriteByte('?')
		return
	}
	if *p {
		b.WriteByte('1')
		return
	}
	b.WriteByte('0')
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
		changed, ok := watchedSourceChanged(entry, path)
		if !ok {
			return entry, nil
		}
		if !changed {
			return entry, nil
		}
		// Mtime changed — rebuild against the resolved path. Callers
		// that passed configPath="" still get the correct rebuild
		// because we route the cached SourcePath through.
		return c.rebuild(ctx, projectID, path)
	}

	return c.rebuild(ctx, projectID, configPath)
}

// ResolveApply is Resolve with a consumer acceptance callback. A changed
// source is rebuilt transactionally and remains unpublished to audit/notice
// consumers until accept returns nil.
func (c *BundleCache) ResolveApply(ctx context.Context, projectID int64, configPath string, accept func(*ProjectRuntime) error) (*ProjectRuntime, bool, error) {
	return c.ResolveApplyWithCommit(ctx, projectID, configPath, func(runtime *ProjectRuntime) (func() error, error) {
		if accept != nil {
			if err := accept(runtime); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
}

// ResolveApplyWithCommit is ResolveApply with a staged consumer commit.
func (c *BundleCache) ResolveApplyWithCommit(ctx context.Context, projectID int64, configPath string, accept func(*ProjectRuntime) (func() error, error)) (*ProjectRuntime, bool, error) {
	c.rebuildMu.Lock()
	defer c.rebuildMu.Unlock()
	c.mu.RLock()
	entry := c.entries[projectID]
	c.mu.RUnlock()
	if entry == nil {
		runtime, err := c.applyLocked(ctx, projectID, configPath, accept)
		return runtime, err == nil, err
	}
	path := entry.SourcePath
	if path == "" {
		path = configPath
	}
	if path == "" {
		return entry, false, nil
	}
	changed, ok := watchedSourceChanged(entry, path)
	if !ok || !changed {
		return entry, false, nil
	}
	runtime, err := c.applyLocked(ctx, projectID, path, accept)
	return runtime, err == nil, err
}

func watchedSourceChanged(entry *ProjectRuntime, fallbackPath string) (changed bool, ok bool) {
	paths := entry.SourcePaths
	if len(paths) == 0 {
		paths = []string{fallbackPath}
	}
	for _, path := range paths {
		if path == "" {
			continue
		}
		// When a watched file is missing (user moved or renamed it after boot),
		// keep serving the cached entry. Explicit Reload surfaces the failure.
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		ok = true
		want, exists := entry.SourceMtimes[path]
		if !exists && path == entry.SourcePath {
			want = entry.Mtime
			exists = !entry.Mtime.IsZero()
		}
		if !exists || !info.ModTime().Equal(want) {
			changed = true
		}
	}
	return changed, ok
}

func statSourceMtimes(paths []string, primary string) (map[string]time.Time, time.Time) {
	out := make(map[string]time.Time, len(paths))
	var primaryMtime time.Time
	for _, path := range paths {
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		mtime := info.ModTime()
		out[path] = mtime
		if path == primary {
			primaryMtime = mtime
		}
	}
	return out, primaryMtime
}

// Reload forces a rebuild of the runtime for projectID regardless of
// the cached entry's mtime. Used by the TUI hot-reload path and by
// tests that need to confirm the rebuild stops the previous engine
// before the new one starts.
func (c *BundleCache) Reload(ctx context.Context, projectID int64, configPath string) (*ProjectRuntime, error) {
	return c.rebuild(ctx, projectID, configPath)
}

// Apply builds a candidate runtime, lets the consumer validate it, and commits
// Store settings and runtime lifecycle only when validation succeeds. The
// consumer callback must not mutate live consumer state; use ApplyWithCommit
// when a consumer needs to stage and publish state atomically.
func (c *BundleCache) Apply(ctx context.Context, projectID int64, configPath string, accept func(*ProjectRuntime) error) (*ProjectRuntime, error) {
	return c.ApplyWithCommit(ctx, projectID, configPath, func(runtime *ProjectRuntime) (func() error, error) {
		if accept != nil {
			if err := accept(runtime); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
}

// ApplyWithCommit implements candidate application as prepare, accept, and
// commit. Preparation has no Store or event-bus side effects. The accept
// callback may build staged consumer state and returns a commit callback that
// is run only after Store settings and runtime lifecycle have committed.
//
// The commit callback must be infallible once returned. TUI callers satisfy
// this by preparing a complete Model value and swapping it in one assignment.
func (c *BundleCache) ApplyWithCommit(ctx context.Context, projectID int64, configPath string, accept func(*ProjectRuntime) (func() error, error)) (*ProjectRuntime, error) {
	c.rebuildMu.Lock()
	defer c.rebuildMu.Unlock()
	return c.applyLocked(ctx, projectID, configPath, accept)
}

// Install seeds the cache with a runtime that was built outside the
// cache (e.g. by Open during boot). The Mtime is captured here so the
// next Resolve can stat-detect changes against the right baseline.
//
// Replacement is serialized with rebuilds. The previous engine is drained
// before the supplied runtime starts and becomes visible; an error leaves the
// previous (now quiescing or stopped) entry installed.
func (c *BundleCache) Install(projectID int64, runtime *ProjectRuntime) error {
	c.rebuildMu.Lock()
	defer c.rebuildMu.Unlock()
	return c.replaceRuntime(context.Background(), projectID, runtime)
}

// Size reports the number of cached entries. Exposed primarily for
// tests that want to assert the Phase 3a invariant (cache size = 1).
func (c *BundleCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// rebuild parses the bundle at configPath and builds a fresh inactive
// ProjectRuntime. Rebuilds are serialized so two concurrent reloads cannot
// publish overlapping engines. Once construction succeeds, the previous
// engine closes admission and drains; only then does the replacement subscribe
// and become visible in the cache.
func (c *BundleCache) rebuild(ctx context.Context, projectID int64, configPath string) (*ProjectRuntime, error) {
	c.rebuildMu.Lock()
	defer c.rebuildMu.Unlock()
	return c.applyLocked(ctx, projectID, configPath, nil)
}

func (c *BundleCache) applyLocked(ctx context.Context, projectID int64, configPath string, accept func(*ProjectRuntime) (func() error, error)) (*ProjectRuntime, error) {

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

	c.mu.RLock()
	old := c.entries[projectID]
	c.mu.RUnlock()
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
	var commitConsumer func() error
	if accept != nil {
		var err error
		commitConsumer, err = accept(runtime)
		if err != nil {
			return nil, err
		}
	}
	if err := c.commitRuntime(ctx, projectID, old, runtime); err != nil {
		return nil, err
	}
	c.commitReload(ctx, projectID, old, runtime)
	if commitConsumer != nil {
		if err := commitConsumer(); err != nil {
			return nil, fmt.Errorf("bundle cache: commit consumer: %w", err)
		}
	}
	return runtime, nil
}

func (c *BundleCache) commitReload(ctx context.Context, projectID int64, old, runtime *ProjectRuntime) {
	if runtime != nil && runtime.BundleImportedPayload != "" && c.store != nil {
		// bundle.imported is a lifecycle event for the runtime that imported
		// the bundle. It uses entity_type=system, but that does not make its
		// project scope global. Zero remains reserved for an explicitly
		// global/bootstrap runtime; a project runtime must pass its own id.
		_ = c.store.RecordEntityEvent(ctx, domain.EventEntitySystem, 0, projectID, domain.EventTypeBundleImported, runtime.BundleImportedPayload)
	}
	if old != nil {
		c.maybeEmitSubtaskKitNotice(ctx, projectID, old.Snapshot, runtime.Snapshot)
		c.maybeNotifySurfacesChanged(old, runtime)
	}
}

func (c *BundleCache) commitRuntime(ctx context.Context, projectID int64, old, runtime *ProjectRuntime) error {
	if old != nil && old.HooksEngine != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, hooks.DefaultShutdownTimeout)
		err := old.HooksEngine.Shutdown(shutdownCtx)
		cancel()
		if err != nil {
			return fmt.Errorf("bundle cache: quiesce previous hooks engine: %w", err)
		}
	}
	if err := c.store.ApplyConfig(ctx, runtime.StoreConfig); err != nil {
		return fmt.Errorf("bundle cache: commit Store settings: %w", err)
	}
	if _, err := c.store.BackfillTaskCompletedAt(ctx, projectID, runtime.Snapshot); err != nil {
		// This is idempotent maintenance. A failed backfill must not make a
		// fully accepted runtime unavailable.
		slog.Warn("bundle cache: completion timestamp backfill failed", "project_id", projectID, "err", err)
	}
	if runtime.HooksEngine != nil && c.bus != nil {
		runtime.HooksEngine.Start(c.bus)
	}
	c.mu.Lock()
	c.entries[projectID] = runtime
	c.mu.Unlock()
	return nil
}

// replaceRuntime performs the reload handoff. Callers serialize invocations
// with rebuildMu. On a drain timeout the replacement remains inactive and
// unpublished, while the old entry remains installed with admission closed;
// retrying after the old action returns completes the handoff safely.
func (c *BundleCache) replaceRuntime(ctx context.Context, projectID int64, runtime *ProjectRuntime) error {
	c.mu.RLock()
	old := c.entries[projectID]
	c.mu.RUnlock()

	if old != nil && old != runtime && old.HooksEngine != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, hooks.DefaultShutdownTimeout)
		err := old.HooksEngine.Shutdown(shutdownCtx)
		cancel()
		if err != nil {
			return fmt.Errorf("bundle cache: quiesce previous hooks engine: %w", err)
		}
	}
	if runtime != nil && runtime.HooksEngine != nil && c.bus != nil {
		runtime.HooksEngine.Start(c.bus)
	}

	c.mu.Lock()
	c.entries[projectID] = runtime
	c.mu.Unlock()
	return nil
}

// maybeEmitSubtaskKitNotice records the one-shot transparency notice
// event when the prev→curr snapshot pair represents a first-enablement
// of subtask_kit (no sub-kit → some configured path). Same-path
// reloads, sub-kit swaps between two paths, and disable transitions all
// fall through without emitting. Errors are swallowed: the rotation
// itself already succeeded and a missing audit row must not abort the
// hot-reload path.
func (c *BundleCache) maybeEmitSubtaskKitNotice(ctx context.Context, projectID int64, prev, curr *config.Snapshot) {
	if !config.NewSubtaskKitNoticeNeeded(prev, curr) {
		return
	}
	if c.store == nil {
		return
	}
	raw, err := (domain.SubtaskKitNoticePayload{
		I18nKey: config.SubtaskKitTransparencyNoticeKey(),
		FromKit: snapshotRootKitKey(prev),
		ToKit:   snapshotSubtaskKitKey(curr),
	}).JSON()
	if err != nil {
		slog.Warn("subtask_kit notice payload marshal failed", "project_id", projectID, "err", err)
		return
	}
	if recErr := c.store.RecordEntityEvent(ctx, domain.EventEntitySystem, 0, projectID, domain.EventTypeSubtaskKitNoticeEmitted, raw); recErr != nil {
		// Audit-row write failure is degraded observability, not a
		// hot-reload failure. Bundle rotation already succeeded; abort
		// would leave the runtime in an inconsistent state. Surface
		// via slog so operators see the gap. Review finding §C.9 of #297.
		slog.Warn("subtask_kit notice audit write failed; transparency notice not persisted",
			"project_id", projectID,
			"err", recErr,
		)
	}
}

// snapshotRootKitKey returns the root kit identity of the snapshot, or
// the empty string when the snapshot is nil. Used by the transparency
// notice payload to attribute the "from" side of a first-enablement
// transition — the previous snapshot has no sub-kit by definition, so
// only the root identity is meaningful.
func snapshotRootKitKey(snap *config.Snapshot) string {
	if snap == nil {
		return ""
	}
	return snap.Kit().Key
}

// snapshotSubtaskKitKey returns the sub-kit identity of the snapshot
// when configured; falls back to the root identity when the sub-kit is
// absent (defensive — the notice only fires when the curr snapshot has
// a sub-kit, but keeping the fallback explicit avoids relying on
// invariants the future could change).
func snapshotSubtaskKitKey(snap *config.Snapshot) string {
	if snap == nil {
		return ""
	}
	if sub, ok := snap.SubtaskKit(); ok {
		return sub.Kit().Key
	}
	return snap.Kit().Key
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
// selector flows into the constructed operation.Service so calls without
// explicit project arguments still see the boot-resolved project /
// CWD; pass a zero value when callers always provide selectors per
// call.
func BuildProjectRuntime(ctx context.Context, store *sqlite.Store, cs *configstore.Adapter, bus events.Bus, configPath string, projectID int64, selector operation.ProjectSelector) (*ProjectRuntime, error) {
	runtime, err := buildProjectRuntime(ctx, store, cs, bus, configPath, projectID, selector)
	if err != nil {
		return nil, err
	}
	if err := store.ApplyConfig(ctx, runtime.StoreConfig); err != nil {
		return nil, err
	}
	if _, err := store.BackfillTaskCompletedAt(ctx, projectID, runtime.Snapshot); err != nil {
		slog.Warn("build project runtime: completion timestamp backfill failed", "project_id", projectID, "err", err)
	}
	if bus != nil {
		runtime.HooksEngine.Start(bus)
	}
	return runtime, nil
}

func buildProjectRuntime(ctx context.Context, store *sqlite.Store, cs *configstore.Adapter, bus events.Bus, configPath string, projectID int64, selector operation.ProjectSelector) (*ProjectRuntime, error) {
	bundle, bundleHash, enumRegistry, err := app.NewConfigService(cs).Import(activity.WithoutTracking(ctx), configPath)
	if err != nil {
		return nil, err
	}
	// Compose the bundle.imported audit payload here, but commit it only after
	// cache publication and consumer rebind succeed. Recording it during build
	// would make a failed candidate appear in the live audit stream.
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

	knownEvents := config.KnownEventsFromDefinitions(bundle.Config.Events.Definitions)
	if err := config.ValidateHooks(bundle.Config.Hooks, knownEvents, func(name string) bool {
		_, ok := registry.Get(name)
		return ok
	}, snapshot.Notifications()); err != nil {
		return nil, err
	}
	if subSnapshot, ok := snapshot.SubtaskKit(); ok {
		subKnownEvents := config.KnownEventsFromDefinitions(subSnapshot.Events().Definitions)
		if err := config.ValidateHooks(subSnapshot.Hooks(), subKnownEvents, func(name string) bool {
			_, ok := registry.Get(name)
			return ok
		}, subSnapshot.Notifications()); err != nil {
			return nil, err
		}
	}

	storeConfig := sqlite.ConfigKnobs{
		BusyTimeoutMs:            bundle.Config.SQLite.BusyTimeoutMs,
		CacheSizeKB:              bundle.Config.SQLite.CacheSizeKB,
		MmapSizeBytes:            bundle.Config.SQLite.MmapSizeBytes,
		EventsDefaultRecentLimit: bundle.Config.Events.DefaultRecentLimit,
		EventsPolicy:             bundle.Config.Events,
		EventBus:                 bus,
	}
	hookEntries := buildDepthAwareHookEntries(snapshot)
	engine := hooks.NewEngine(hookEntries, registry, snapshot.Events(), store)
	if projectID == hooks.GlobalProjectID {
		engine.SetGlobal()
	} else {
		engine.SetProjectID(projectID)
	}

	// snapshot is built above before the hooks engine — both surfaces
	// consume the same immutable per-project pointer. Each rebuild
	// produces a fresh pointer; in-flight callers that captured the
	// previous pointer continue reading from it until they return.

	svc := operation.NewService(store, selector)
	// SetSnapshot is the single wiring entry point: the agent service
	// derives the catalog closures, synonym table, stopword set, and
	// bundle-scoped EnumRegistry from the per-project Snapshot in one
	// pass. Two projects holding two snapshots see two independent
	// catalog views; hot-reload rotates the pointer atomically through
	// cache.Reload.
	svc.SetSnapshot(snapshot)
	editor := app.NewBundleEditor(cs, configPath)
	svc.SetEntityRepos(editor, cs, cs)
	// Inject the orphan service with prev=nil — the cache rotation
	// overrides this with a rebind-capable view (current+previous
	// snapshots) when the runtime is replacing an earlier entry.
	svc.SetOrphanService(app.NewOrphanService(store, snapshot, nil))
	// Best-effort backfill: populate tasks.completed_at for historical
	// done-bucket rows missing the timestamp. Idempotent — subsequent
	// builds find no rows to update. Errors are swallowed so a hot
	// transient (FK lock, etc.) cannot block runtime composition.
	svc.SetSettings(operation.ServiceSettings{
		RecentCommentLimit:       bundle.Config.MCP.RecentCommentLimit,
		MaxCommentChars:          bundle.Config.MCP.MaxCommentChars,
		IncludeWorkflow:          *bundle.Config.MCP.IncludeWorkflowInContinue,
		CachePrompts:             *bundle.Config.MCP.CachePrompts,
		NextWorkLimit:            bundle.Config.MCP.NextWorkLimit,
		SimilarTaskLimit:         bundle.Config.MCP.SimilarTaskLimit,
		SolutionsTopLimitDefault: bundle.Config.Solutions.DefaultTopLimit,
		SolutionsTopLimitMax:     bundle.Config.Solutions.MaxTopLimit,
	})

	sourcePaths := bundle.SourcePaths
	if len(sourcePaths) == 0 {
		sourcePaths = []string{configPath}
	}
	sourceMtimes, mtime := statSourceMtimes(sourcePaths, configPath)

	return &ProjectRuntime{
		Service:               svc,
		HooksEngine:           engine,
		ActionRegistry:        registry,
		NotificationAction:    notificationAction,
		EnumRegistry:          enumRegistry,
		NotificationSnapshot:  notifSnapshot,
		SourcePath:            configPath,
		SourcePaths:           append([]string(nil), sourcePaths...),
		LoadedAt:              time.Now(),
		Mtime:                 mtime,
		SourceMtimes:          sourceMtimes,
		Snapshot:              snapshot,
		Workflow:              app.NewWorkflowServiceFromStore(store, snapshot.Registry(), snapshot),
		Editor:                editor,
		BundleImportedPayload: string(auditPayload),
		StoreConfig:           storeConfig,
		// PreviousSnapshot is populated by the cache on rotation —
		// buildProjectRuntime has no access to the prior entry.
	}, nil
}
