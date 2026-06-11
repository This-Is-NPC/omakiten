package agentruntime

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"omakiten/internal/agent"
	"omakiten/internal/config"
	"omakiten/internal/paths"
)

// TestBundleCacheHitReturnsSamePointer asserts the Phase 3a invariant:
// repeated Resolve calls on the same project id without filesystem
// changes return the identical *ProjectRuntime. A different pointer
// would mean either a torn cache (concurrent rebuild) or a missing
// mtime short-circuit — both regressions the cache exists to prevent.
func TestBundleCacheHitReturnsSamePointer(t *testing.T) {
	ctx := context.Background()
	rt := openTestRuntime(t)
	defer func() { _ = rt.Close() }()

	cache := rt.Cache()
	if cache == nil {
		t.Fatal("Runtime.Cache() = nil")
	}
	first, err := cache.Resolve(ctx, rt.defaultProjectID, rt.configPath)
	if err != nil {
		t.Fatalf("Resolve #1: %v", err)
	}
	second, err := cache.Resolve(ctx, rt.defaultProjectID, rt.configPath)
	if err != nil {
		t.Fatalf("Resolve #2: %v", err)
	}
	if first != second {
		t.Fatalf("cache miss on second Resolve: first=%p second=%p", first, second)
	}
	if cache.Size() != 1 {
		t.Fatalf("Phase 3a invariant: cache size = %d, want 1", cache.Size())
	}
}

// TestBundleCacheMtimeChangeTriggersRebuild bumps the source bundle's
// mtime and confirms Resolve returns a fresh runtime. The previous
// engine must also be Stop()ed so the new one can subscribe to the
// shared bus without two listeners stealing each other's events.
func TestBundleCacheMtimeChangeTriggersRebuild(t *testing.T) {
	ctx := context.Background()
	rt := openTestRuntime(t)
	defer func() { _ = rt.Close() }()

	cache := rt.Cache()
	first, err := cache.Resolve(ctx, rt.defaultProjectID, rt.configPath)
	if err != nil {
		t.Fatalf("Resolve #1: %v", err)
	}

	// Advance the source mtime to one second in the future. Filesystem
	// resolution is second-grained on most filesystems, so anything
	// smaller risks the stat returning the same Mtime and the test
	// silently passing for the wrong reason.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(rt.configPath, future, future); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	second, err := cache.Resolve(ctx, rt.defaultProjectID, rt.configPath)
	if err != nil {
		t.Fatalf("Resolve #2: %v", err)
	}
	if first == second {
		t.Fatalf("Resolve did not rebuild on mtime change: same pointer %p", first)
	}
	if cache.Size() != 1 {
		t.Fatalf("cache size after rebuild = %d, want 1", cache.Size())
	}
}

func TestRuntimeServiceResolvesMtimeChanges(t *testing.T) {
	ctx := context.Background()
	rt := openTestRuntime(t)
	defer func() { _ = rt.Close() }()

	first := rt.Service()
	if first == nil {
		t.Fatal("Service() = nil")
	}
	resp, err := first.ResolveCommand(ctx, agent.ResolveCommandInput{Name: "okt"})
	if err != nil {
		t.Fatalf("ResolveCommand before edit: %v", err)
	}
	if resp.AgentOutputLanguage != "" {
		t.Fatalf("initial agent output language = %q, want empty", resp.AgentOutputLanguage)
	}

	bundle, err := config.LoadBundle(rt.configPath)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	bundle.Config.Languages.AgentOutput = "Português (Brasil)"
	if err := config.SaveBundle(rt.configPath, bundle); err != nil {
		t.Fatalf("SaveBundle: %v", err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(rt.configPath, future, future); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	second := rt.Service()
	if second == nil {
		t.Fatal("Service() after edit = nil")
	}
	if second == first {
		t.Fatal("Service() returned stale service after config mtime changed")
	}
	resp, err = second.ResolveCommand(ctx, agent.ResolveCommandInput{Name: "okt"})
	if err != nil {
		t.Fatalf("ResolveCommand after edit: %v", err)
	}
	if resp.AgentOutputLanguage != "Português (Brasil)" {
		t.Fatalf("agent output language after reload = %q, want Português (Brasil)", resp.AgentOutputLanguage)
	}
}

func TestRuntimeServiceResolvesActiveProfileSwitch(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	t.Setenv(paths.HomeEnv, tmp)

	omakase, err := config.SeedInstall(tmp, "omakase", true)
	if err != nil {
		t.Fatalf("SeedInstall omakase: %v", err)
	}
	kaiseki, err := config.SeedInstall(tmp, "kaiseki", false)
	if err != nil {
		t.Fatalf("SeedInstall kaiseki: %v", err)
	}
	bundle, err := config.LoadBundle(kaiseki.Path)
	if err != nil {
		t.Fatalf("LoadBundle kaiseki: %v", err)
	}
	bundle.Config.Languages.AgentOutput = "Português (Brasil)"
	if err := config.SaveBundle(kaiseki.Path, bundle); err != nil {
		t.Fatalf("SaveBundle kaiseki: %v", err)
	}
	if err := paths.SetActiveConfigInDir(filepath.Join(tmp, "config"), filepath.Base(omakase.Path)); err != nil {
		t.Fatalf("SetActiveConfig omakase: %v", err)
	}

	rt, err := Open(ctx, Options{DBPath: filepath.Join(tmp, "omakiten.db"), CWD: tmp})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = rt.Close() }()

	first := rt.Service()
	if first == nil {
		t.Fatal("Service() = nil")
	}
	resp, err := first.ResolveCommand(ctx, agent.ResolveCommandInput{Name: "okt"})
	if err != nil {
		t.Fatalf("ResolveCommand before active switch: %v", err)
	}
	if resp.AgentOutputLanguage != "" {
		t.Fatalf("initial agent output language = %q, want empty", resp.AgentOutputLanguage)
	}

	if err := paths.SetActiveConfigInDir(filepath.Join(tmp, "config"), filepath.Base(kaiseki.Path)); err != nil {
		t.Fatalf("SetActiveConfig kaiseki: %v", err)
	}
	second := rt.Service()
	if second == nil {
		t.Fatal("Service() after active switch = nil")
	}
	if second == first {
		t.Fatal("Service() returned stale service after active profile switched")
	}
	resp, err = second.ResolveCommand(ctx, agent.ResolveCommandInput{Name: "okt"})
	if err != nil {
		t.Fatalf("ResolveCommand after active switch: %v", err)
	}
	if resp.AgentOutputLanguage != "Português (Brasil)" {
		t.Fatalf("agent output language after active switch = %q, want Português (Brasil)", resp.AgentOutputLanguage)
	}
}

// TestRuntimeServicePreservesPriorRuntimeOnInvalidInPlaceEdit pins the
// AC3 contract: when the active config file is overwritten in place with
// malformed YAML and its mtime bumped, Service() must keep returning the
// PRIOR working service (still resolving the OLD settings) rather than
// nil/panicking or surfacing the broken bundle. The reload path is
// Service() -> Resolve -> rebuild; rebuild's buildProjectRuntime fails to
// parse, returns an error WITHOUT swapping c.entries, so Resolve bubbles
// the error and Service() falls back to cache.Get -> the prior pr.Service.
func TestRuntimeServicePreservesPriorRuntimeOnInvalidInPlaceEdit(t *testing.T) {
	ctx := context.Background()
	rt := openTestRuntime(t)
	defer func() { _ = rt.Close() }()

	first := rt.Service()
	if first == nil {
		t.Fatal("Service() = nil")
	}
	resp, err := first.ResolveCommand(ctx, agent.ResolveCommandInput{Name: "okt"})
	if err != nil {
		t.Fatalf("ResolveCommand before edit: %v", err)
	}
	if resp.AgentOutputLanguage != "" {
		t.Fatalf("initial agent output language = %q, want empty", resp.AgentOutputLanguage)
	}

	// Overwrite the active source in place with unparseable YAML, then
	// bump its mtime so Resolve attempts a rebuild on the next call.
	if err := os.WriteFile(rt.configPath, []byte("::: not: [valid yaml"), 0o644); err != nil {
		t.Fatalf("write malformed bundle: %v", err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(rt.configPath, future, future); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	second := rt.Service()
	if second == nil {
		t.Fatal("Service() after invalid in-place edit = nil; broken bundle must not drop the prior runtime")
	}
	if second != first {
		t.Fatal("Service() swapped to a different service after an invalid edit; rebuild must not replace c.entries on parse failure")
	}
	// The prior service still resolves the OLD (empty) language — the
	// broken bundle was never installed.
	resp, err = second.ResolveCommand(ctx, agent.ResolveCommandInput{Name: "okt"})
	if err != nil {
		t.Fatalf("ResolveCommand after invalid edit: %v", err)
	}
	if resp.AgentOutputLanguage != "" {
		t.Fatalf("agent output language after invalid edit = %q, want empty (prior settings preserved)", resp.AgentOutputLanguage)
	}
}

// TestRuntimeServiceExplicitConfigIgnoresActiveSwitch pins the AC5
// contract: a caller that opened the runtime with an explicit
// Options.ConfigPath (configPathExplicit) must NOT be redirected when the
// active-profile marker (.active) flips to a different profile. Service()
// short-circuits the marker re-resolve for explicit callers and keeps
// resolving the path it was given.
func TestRuntimeServiceExplicitConfigIgnoresActiveSwitch(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	t.Setenv(paths.HomeEnv, tmp)

	omakase, err := config.SeedInstall(tmp, "omakase", true)
	if err != nil {
		t.Fatalf("SeedInstall omakase: %v", err)
	}
	kaiseki, err := config.SeedInstall(tmp, "kaiseki", false)
	if err != nil {
		t.Fatalf("SeedInstall kaiseki: %v", err)
	}
	// Give kaiseki a distinct language so a wrong redirect would be
	// observable in the resolved command.
	bundle, err := config.LoadBundle(kaiseki.Path)
	if err != nil {
		t.Fatalf("LoadBundle kaiseki: %v", err)
	}
	bundle.Config.Languages.AgentOutput = "Português (Brasil)"
	if err := config.SaveBundle(kaiseki.Path, bundle); err != nil {
		t.Fatalf("SaveBundle kaiseki: %v", err)
	}
	if err := paths.SetActiveConfigInDir(filepath.Join(tmp, "config"), filepath.Base(omakase.Path)); err != nil {
		t.Fatalf("SetActiveConfig omakase: %v", err)
	}

	// Open with an EXPLICIT ConfigPath (omakase) — this sets
	// configPathExplicit so the marker re-resolve is bypassed.
	rt, err := Open(ctx, Options{
		DBPath:     filepath.Join(tmp, "omakiten.db"),
		ConfigPath: omakase.Path,
		CWD:        tmp,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = rt.Close() }()

	first := rt.Service()
	if first == nil {
		t.Fatal("Service() = nil")
	}
	resp, err := first.ResolveCommand(ctx, agent.ResolveCommandInput{Name: "okt"})
	if err != nil {
		t.Fatalf("ResolveCommand before active switch: %v", err)
	}
	if resp.AgentOutputLanguage != "" {
		t.Fatalf("initial agent output language = %q, want empty (explicit omakase)", resp.AgentOutputLanguage)
	}

	// Flip the active marker to kaiseki. An explicit caller must ignore
	// this — Service() keeps resolving the omakase path it was opened with.
	if err := paths.SetActiveConfigInDir(filepath.Join(tmp, "config"), filepath.Base(kaiseki.Path)); err != nil {
		t.Fatalf("SetActiveConfig kaiseki: %v", err)
	}
	second := rt.Service()
	if second == nil {
		t.Fatal("Service() after active switch = nil")
	}
	resp, err = second.ResolveCommand(ctx, agent.ResolveCommandInput{Name: "okt"})
	if err != nil {
		t.Fatalf("ResolveCommand after active switch: %v", err)
	}
	if resp.AgentOutputLanguage != "" {
		t.Fatalf("explicit --config caller was redirected by the active marker: language = %q, want empty (omakase)", resp.AgentOutputLanguage)
	}
}

// TestBundleCacheReloadForcesRebuild bypasses the mtime check and
// confirms Reload always returns a fresh runtime. Used by the TUI
// hot-reload path where the user picked a different config without
// touching the file system.
func TestBundleCacheReloadForcesRebuild(t *testing.T) {
	ctx := context.Background()
	rt := openTestRuntime(t)
	defer func() { _ = rt.Close() }()

	cache := rt.Cache()
	first, err := cache.Resolve(ctx, rt.defaultProjectID, rt.configPath)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	second, err := cache.Reload(ctx, rt.defaultProjectID, rt.configPath)
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if first == second {
		t.Fatalf("Reload returned cached pointer instead of rebuilding")
	}
}

// TestBundleCacheConcurrentResolveSafe runs many goroutines through
// Resolve to surface any race condition the RWMutex would miss. The
// test does not assert pointer identity (a rebuild could interleave
// with another reader); only that no goroutine errors and the final
// size still satisfies the Phase 3a invariant.
func TestBundleCacheConcurrentResolveSafe(t *testing.T) {
	ctx := context.Background()
	rt := openTestRuntime(t)
	defer func() { _ = rt.Close() }()

	const readers = 32
	const iters = 50

	var wg sync.WaitGroup
	errs := make(chan error, readers)
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				if _, err := rt.Cache().Resolve(ctx, rt.defaultProjectID, rt.configPath); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Resolve error: %v", err)
		}
	}
	if rt.Cache().Size() != 1 {
		t.Fatalf("cache size after concurrent reads = %d, want 1", rt.Cache().Size())
	}
}

// TestBundleCacheResolveEmptyPathRebuildOnMtimeChange pins the bug
// fix for Resolve(ctx, id, "") with mtime drift on a cached entry.
// Before the fix, Resolve routed the original empty configPath into
// rebuild and bailed with "bundle cache: configPath is required";
// after, Resolve falls back to the cached SourcePath so the rebuild
// succeeds.
func TestBundleCacheResolveEmptyPathRebuildOnMtimeChange(t *testing.T) {
	ctx := context.Background()
	rt := openTestRuntime(t)
	defer func() { _ = rt.Close() }()

	cache := rt.Cache()
	first, err := cache.Resolve(ctx, rt.defaultProjectID, rt.configPath)
	if err != nil {
		t.Fatalf("Resolve seed: %v", err)
	}

	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(rt.configPath, future, future); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	second, err := cache.Resolve(ctx, rt.defaultProjectID, "")
	if err != nil {
		t.Fatalf("Resolve with empty configPath after mtime change: %v", err)
	}
	if first == second {
		t.Fatal("Resolve did not rebuild against cached SourcePath")
	}
	if second.SourcePath != rt.configPath {
		t.Fatalf("rebuild SourcePath = %q, want %q", second.SourcePath, rt.configPath)
	}
}

// TestBundleCachePerProjectNotificationActionIsolation pins the Phase
// 3d invariant: two ProjectRuntime entries (different project ids)
// hold distinct ActionRegistry and NotificationShowAction instances.
// The engine.projectID filter alone is not enough; if both engines
// dispatched against the same NotificationShowAction, a project A
// notification slug would resolve against B's catalog. Distinct
// actions with distinct snapshot pointers prove isolation at the
// action layer.
func TestBundleCachePerProjectNotificationActionIsolation(t *testing.T) {
	ctx := context.Background()
	rt := openTestRuntime(t)
	defer func() { _ = rt.Close() }()

	cache := rt.Cache()
	prA, err := cache.Resolve(ctx, rt.defaultProjectID, rt.configPath)
	if err != nil {
		t.Fatalf("Resolve A: %v", err)
	}
	// Resolve a second project entry against the same source so we
	// exercise the registry/snapshot isolation independent of bundle
	// content. Two distinct entries on the same yaml is the minimum
	// shape the cache supports today.
	prB, err := cache.Resolve(ctx, rt.defaultProjectID+1, rt.configPath)
	if err != nil {
		t.Fatalf("Resolve B: %v", err)
	}
	if prA == prB {
		t.Fatal("Resolve returned the same entry for different ids")
	}
	if prA.NotificationAction == nil || prB.NotificationAction == nil {
		t.Fatal("ProjectRuntime missing NotificationAction")
	}
	if prA.NotificationAction == prB.NotificationAction {
		t.Fatal("per-project NotificationAction pointer aliased — actions would cross-fire on shared sender")
	}
	if prA.ActionRegistry == prB.ActionRegistry {
		t.Fatal("per-project ActionRegistry pointer aliased — Register on A would leak into B")
	}
	if prA.HooksEngine == prB.HooksEngine {
		t.Fatal("per-project HooksEngine pointer aliased")
	}
}

// TestBundleCacheReloadPreservesProjectSelector pins the Phase 3a
// invariant the original ship missed: a mtime-driven Reload must
// carry the boot-resolved ProjectSelector into the freshly built
// agent.Service. Without it, calls without explicit project args
// fall to a zero selector after the first rebuild.
func TestBundleCacheReloadPreservesProjectSelector(t *testing.T) {
	ctx := context.Background()
	rt := openTestRuntime(t)
	defer func() { _ = rt.Close() }()

	bootSelector := rt.Service().Selector()
	if bootSelector.CWD == "" {
		t.Fatal("test fixture sanity: boot selector CWD empty")
	}

	if _, err := rt.Cache().Reload(ctx, rt.defaultProjectID, rt.configPath); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	got := rt.Service().Selector()
	if got != bootSelector {
		t.Fatalf("Reload lost selector: got %+v want %+v", got, bootSelector)
	}
}

// TestPerProjectIsolation pins the Phase 2-bis core invariant: two
// ProjectRuntime entries each carry their own *config.Snapshot
// pointer. Even on the same source bundle, the cache must build a
// fresh snapshot per entry — pointer aliasing would re-introduce the
// shared-singleton bug the InMemoryProviders model produced.
//
// The agent service captured by each entry reads through SetSnapshot,
// so distinct *Snapshot pointers guarantee distinct catalog views
// without any further plumbing. Bucket lookups go through the
// snapshot directly; this test confirms the snapshot's identity (not
// just contents) differs across projects.
func TestPerProjectIsolation(t *testing.T) {
	ctx := context.Background()
	rt := openTestRuntime(t)
	defer func() { _ = rt.Close() }()

	cache := rt.Cache()
	prA, err := cache.Resolve(ctx, rt.defaultProjectID, rt.configPath)
	if err != nil {
		t.Fatalf("Resolve A: %v", err)
	}
	prB, err := cache.Resolve(ctx, rt.defaultProjectID+1, rt.configPath)
	if err != nil {
		t.Fatalf("Resolve B: %v", err)
	}

	if prA == prB {
		t.Fatal("cache returned the same ProjectRuntime for different ids")
	}
	if prA.Snapshot == nil || prB.Snapshot == nil {
		t.Fatal("ProjectRuntime missing Snapshot pointer")
	}
	if prA.Snapshot == prB.Snapshot {
		t.Fatal("per-project Snapshot pointer aliased — workflow / catalogs / synonyms would cross-fire across projects")
	}
	if prA.Service == prB.Service {
		t.Fatal("per-project Service pointer aliased — SetSnapshot would clobber the other project")
	}

	// Workflow shape is sourced from the same bundle here, so the
	// content is equivalent — but the lookup must go through the
	// per-project pointer, never through a shared singleton. Confirm
	// both snapshots resolve the workflow's first bucket via their
	// own map (not via the now-deleted Store.Providers()).
	wfA := prA.Snapshot.Workflow()
	wfB := prB.Snapshot.Workflow()
	if len(wfA.Buckets) == 0 || len(wfB.Buckets) == 0 {
		t.Fatalf("workflow buckets empty: A=%+v B=%+v", wfA.Buckets, wfB.Buckets)
	}
	if wfA.Buckets[0].Key != wfB.Buckets[0].Key {
		t.Fatalf("workflow shape diverged across projects without an explicit reload: A=%q B=%q", wfA.Buckets[0].Key, wfB.Buckets[0].Key)
	}
}

// TestHotReloadDoesNotAffectInFlight pins the immutability contract:
// a caller that captured *Snapshot before Reload continues to read
// the prior workflow shape after Reload installs a new pointer. The
// agent service inside that captured runtime holds the old Snapshot,
// so an in-flight MCP call mid-dispatch never observes a torn view of
// the bundle.
//
// The implementation guarantee comes from Snapshot being an immutable
// value-typed pointer (no Swap, no atomic field embedded) plus
// cache.Reload installing a fresh pointer in a new entry rather than
// mutating the previous entry in place.
func TestHotReloadDoesNotAffectInFlight(t *testing.T) {
	ctx := context.Background()
	rt := openTestRuntime(t)
	defer func() { _ = rt.Close() }()

	cache := rt.Cache()
	pre, err := cache.Resolve(ctx, rt.defaultProjectID, rt.configPath)
	if err != nil {
		t.Fatalf("Resolve seed: %v", err)
	}
	if pre.Snapshot == nil {
		t.Fatal("seed ProjectRuntime carries nil Snapshot")
	}
	capturedSnapshot := pre.Snapshot
	capturedWorkflowKey := capturedSnapshot.Workflow().Key

	if _, err := cache.Reload(ctx, rt.defaultProjectID, rt.configPath); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	post := cache.Get(rt.defaultProjectID)
	if post == nil {
		t.Fatal("Get after Reload returned nil")
	}
	if post.Snapshot == capturedSnapshot {
		t.Fatal("Reload returned same Snapshot pointer; rebuild must install a fresh one")
	}
	// Pre-Reload caller's snapshot is still readable and unchanged.
	if got := capturedSnapshot.Workflow().Key; got != capturedWorkflowKey {
		t.Fatalf("pre-Reload snapshot mutated by post-Reload activity: got %q, want %q", got, capturedWorkflowKey)
	}
	// PreviousSnapshot on the new entry must pin the old pointer so
	// the orphan flow can resolve task.bucket_id → previous key.
	if post.PreviousSnapshot != capturedSnapshot {
		t.Fatalf("cache rotation lost PreviousSnapshot: got %p, want %p", post.PreviousSnapshot, capturedSnapshot)
	}
}

// TestConcurrentAgentsDifferentProjects runs many parallel goroutines
// resolving and reading snapshots for two distinct project ids. Under
// `-race`, any cross-project mutation of a Snapshot field would
// surface as a data-race finding. The test does not invoke
// app.WorkflowService.MoveTask end-to-end (that would require a SQL
// fixture per project with separate writers and is covered by the
// integration tests); instead it stresses the cache + snapshot read
// surface that the per-project isolation invariant depends on.
func TestConcurrentAgentsDifferentProjects(t *testing.T) {
	ctx := context.Background()
	rt := openTestRuntime(t)
	defer func() { _ = rt.Close() }()

	cache := rt.Cache()
	if _, err := cache.Resolve(ctx, rt.defaultProjectID, rt.configPath); err != nil {
		t.Fatalf("seed Resolve A: %v", err)
	}
	if _, err := cache.Resolve(ctx, rt.defaultProjectID+1, rt.configPath); err != nil {
		t.Fatalf("seed Resolve B: %v", err)
	}

	const readers = 16
	const iters = 50
	var wg sync.WaitGroup
	errs := make(chan error, readers)
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			projectID := rt.defaultProjectID
			if i%2 == 1 {
				projectID = rt.defaultProjectID + 1
			}
			for j := 0; j < iters; j++ {
				pr, err := cache.Resolve(ctx, projectID, rt.configPath)
				if err != nil {
					errs <- err
					return
				}
				if pr.Snapshot == nil {
					errs <- &nilSnapshotErr{projectID: projectID}
					return
				}
				// Read every catalog surface so the race detector
				// inspects the snapshot's internal maps under
				// concurrent dispatch.
				_ = pr.Snapshot.Workflow()
				_ = pr.Snapshot.Personas()
				_ = pr.Snapshot.Skills()
				_ = pr.Snapshot.Laws()
				_ = pr.Snapshot.Templates()
				_ = pr.Snapshot.MCPCommands()
				_ = pr.Snapshot.Synonyms()
				_ = pr.Snapshot.Stopwords()
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent snapshot read error: %v", err)
		}
	}
	if cache.Size() != 2 {
		t.Fatalf("cache size after concurrent reads = %d, want 2", cache.Size())
	}
}

type nilSnapshotErr struct{ projectID int64 }

func (e *nilSnapshotErr) Error() string {
	return "Snapshot missing on cache entry"
}

func openTestRuntime(t *testing.T) *Runtime {
	t.Helper()
	ctx := context.Background()
	tmp := t.TempDir()
	rt, err := Open(ctx, Options{
		DBPath:     filepath.Join(tmp, "omakiten.db"),
		ConfigPath: filepath.Join(tmp, "config", "omakase.yaml"),
		CWD:        tmp,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return rt
}
