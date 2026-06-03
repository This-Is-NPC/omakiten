# Project overrides — per-project bundles and snapshot layering

Authoritative reference for how Omakiten layers configuration per
project: how `.omakiten/` is discovered, how each project gets its own
immutable `*config.Snapshot` in memory, and how hot-reload swaps a
fresh pointer without poisoning in-flight calls. Captures the
architectural shape #117 (Phase 2-bis) settled on and the drift from
#110 that motivated it.

Audience: configurators wanting to override defaults for a single
project, and contributors editing the agent runtime, the SQL adapter,
or any service that reads workflow shape / catalogs / settings.
Reading this file before changing those paths prevents reverting
accidentally to the shared-singleton model the migration retired.

For ConfigRoot precedence and the on-disk layout, see
[path-resolution.md](path-resolution.md). For top-level config
knobs, see [system.md](system.md).

## TL;DR

- Each project gets its own immutable `*config.Snapshot` pointer, owned
  by `agentruntime.ProjectRuntime`.
- The Snapshot is built once per bundle import; mutation flows through
  `BundleCache` producing a fresh pointer on rebuild, not by Swap on the
  existing one.
- App services capture the pointer at construction (the agent service
  via `SetSnapshot`; every file-CRUD service through its constructor).
  Hot-reload returns a brand-new `*ProjectRuntime` with rebuilt services
  bound to the rotated Snapshot — callers do not re-fetch per call. N
  projects in the cache ⇒ N independent snapshots ⇒ zero cross-project
  leakage on hot paths.
- The SQL adapter (`*sqlite.Store`) is back to operational state only:
  `tasks`, `events` (with `event_tags`), `task_dependencies`, `tags`
  (with `task_tags` / `project_tags` / `error_tags`), `errors`,
  `solutions`, `plans`, `plan_waves` (added by
  `migrations/023_plans.sql`; service layer in
  `internal/sqlite/plans.go`). Migration 009 folded `comments` and
  `activity_logs` into `events`; migration 020 dropped every config
  table; migration 022 adds the FTS5 `search_index` virtual table behind
  the unified `search` MCP tool — populated by triggers off the base
  tables, no bundle reads required. The Store carries no bundle field —
  the only bundle-aware emission is
  `Store.RecordEntityEvent(..., EventTypeBundleImported, ...)`
  called by `agentruntime.buildProjectRuntime` before
  `BuildSnapshot`, recording the audit event and writing nothing else.

## Layer diagram

```text
                  ┌──────────────────────────────┐
                  │     omakase.yaml (disk)      │
                  └──────────────┬───────────────┘
                                 │ LoadBundle
                                 ▼
            ┌────────────────────────────────────────┐
            │   internal/config.Bundle (value type)  │
            └──────────────┬─────────────────────────┘
                           │ BuildSnapshot
                           ▼
            ┌────────────────────────────────────────┐
            │  internal/config.Snapshot (immutable)  │
            └──────────────┬─────────────────────────┘
                           │ owned by
                           ▼
            ┌────────────────────────────────────────┐
            │ agentruntime.ProjectRuntime[projectID] │
            │   ├── Snapshot / PreviousSnapshot      │
            │   ├── Service (agent.Service)          │
            │   ├── Workflow (app.WorkflowService)   │
            │   ├── HooksEngine                      │
            │   ├── ActionRegistry                   │
            │   ├── NotificationAction              │
            │   ├── NotificationSnapshot            │
            │   ├── EnumRegistry                     │
            │   ├── Theme                            │
            │   └── SourcePath / LoadedAt / Mtime    │
            └──────────────┬─────────────────────────┘
                           │ SetSnapshot,
                           │ NewService,
                           ▼
            ┌────────────────────────────────────────┐
            │ app services (Workflow / Persona /     │
            │ Skill / Law / Context / TUIQuery /     │
            │ Template …)                            │
            └──────────────┬─────────────────────────┘
                           │ state-only reads + writes
                           ▼
                  ┌──────────────────────┐
                  │   sqlite.Store       │
                  │   (state-only adapter)│
                  └──────────────────────┘
```

## Invariants

The migration enforces six invariants. Code reviews on this area
should reject changes that break any of them.

1. **Store = SQL adapter, period.** No `config.Bundle`,
   `config.Snapshot`, `config.Providers` field on the Store. There is no
   bundle-aware method on `*sqlite.Store` — the `bundle.imported` audit
   event is recorded through the generic `Store.RecordEntityEvent(ctx,
   EventEntitySystem, 0, 0, EventTypeBundleImported, payloadJSON)`
   helper from `agentruntime.buildProjectRuntime`, so the Store never
   special-cases bundle ingestion. Snapshot ownership lives on
   `agentruntime.ProjectRuntime`.
2. **Snapshot is immutable.** No `Swap`, no `atomic.Pointer` embedded,
   no `Set*` mutator. Mutation produces a new pointer; the old one
   keeps serving in-flight callers.
3. **App services receive `*config.Snapshot` at construction.**
   `WorkflowService`, `PersonaService`, `SkillService`, and `LawService`
   all capture the pointer once on construction and
   read through `s.snap.X`. The cache rebuilds the per-project services
   when the bundle changes — a hot-reload returns a fresh
   `*ProjectRuntime` with new service instances bound to the new
   Snapshot, so callers reaching the cache after a reload see the new
   shape while in-flight callers continue to read the previous pointer
   until they release it.
4. **Per-project = `ProjectRuntime` carries its own Snapshot.**
   `BundleCache.Resolve` returns the per-project entry; `Reload`
   produces a new entry with a fresh Snapshot and rotates
   `PreviousSnapshot` onto it. In-flight calls captured the previous
   pointer via `*agent.Service` resolved before the dispatch switch,
   so the rotation never poisons them.
5. **Concurrency = N agents, N ProjectRuntime, zero shared mutable
   hot-path state.** The cache's RWMutex guards the entry map; entries
   themselves are read-only after construction. Guards of project A
   never consult guards of project B; the snapshot pointer A captured
   reaches only A's guards.
6. **Snapshot accessors return defensive copies — except for the
   hot-path readers `Synonyms()` and `Stopwords()`, which return the
   underlying map/slice by reference.** The tag-normalize path
   (`NormalizeTagName` fires per tag per comment / error / task) and
   the search-stopword filter cannot afford a per-call allocation, so
   `BuildSnapshot` materialises one map and one slice the snapshot
   owns and every caller reads through. Contract: **callers must not
   mutate the returned map/slice.** A future caller that needs to
   mutate copies first. Snapshot itself remains immutable — the shared
   pointers are read-only views, not editable handles. Documented on
   the accessor godoc in `internal/config/snapshot.go`.

## Anti-patterns

Captured from the #110 → #117 drift retrospective. Each one was tried
or considered; each breaks an invariant.

### Map per-project on the Store

```go
// NO
type Store struct {
    providersByProject map[int64]*config.Snapshot   // ← NO
    providersMu sync.RWMutex
}
func (s *Store) ActiveWorkflow(ctx, projectID int64) (domain.Workflow, error) {
    return s.providersByProject[projectID].Workflow(), nil
}
```

Why it breaks: Store is still a ConfigRepository. The hexagonal
direction reverses (adapter knowing about config). The correct shape
is: Store has no `ActiveWorkflow` method at all.

### Context-threaded Snapshot with Store still reading

```go
// NO
func (s *Store) ActiveWorkflow(ctx) (domain.Workflow, error) {
    snap := config.FromCtx(ctx)
    return snap.Workflow(), nil
}
```

Why it breaks: Store still "knows about" config. Dependency direction
wrong. Correct shape: the method leaves the Store entirely and becomes
`app.WorkflowService.Active(ctx)` reading from `s.snap`.

### Mutable Snapshot

```go
// NO
type Snapshot struct {
    bundle atomic.Pointer[Bundle]
}
func (s *Snapshot) Swap(b *Bundle) { s.bundle.Store(b) }
```

Why it breaks: Snapshot is no longer immutable. Hot-reload poisons
in-flight calls that captured the pointer. Correct shape: `Snapshot`
is a value-typed struct; the cache produces a fresh pointer and swaps
the *entry*, never the *contents*.

### N per-field setters on agent.Service

```go
// NO
svc.SetWorkflow(snap.Workflow())
svc.SetPersonas(snap.Personas())
svc.SetGuards(snap.Guards)
```

Why it breaks: N setters means easy to forget one next feature.
Correct shape: one `SetSnapshot(*Snapshot)` setter; everything else
derives.

### Keep `bundles.go` "shrunk"

Quoting #110:

> "methods shrunk to thin delegators (~30–90 LOC each); deferred to a
> follow-up that rewires services to call providers directly."

Why it breaks: shrunk delegators still represent the wrong dependency
direction. Correct shape: `bundles.go` becomes the audit-event emitter
plus snapshot rotation, never an `ActiveWorkflow` / `ListActive*`
surface.

### `ConfigRepository` "kept for compat"

```go
// NO
type ConfigRepository interface {
    ActiveWorkflow(ctx) (domain.Workflow, error)   // ← dead
}
```

Why it breaks: the interface declares methods nothing calls. Correct
shape: the interface disappears entirely once ProjectRuntime owns the
snapshot and `Store.RecordEntityEvent(..., EventTypeBundleImported,
...)` (`internal/sqlite/events.go:96`) carries the audit side-effect.

## Appendix — #110 drift retrospective

#117 exists because #110 closed green by labelling load-bearing work
"cosmetic". The exact words in the #110 close-out:

> "Service constructor rewires to take `ProviderSet` directly
> (pragmatic delegation through Store satisfies all criteria; further
> constructor surgery is cosmetic)."
> "File deletion of `internal/sqlite/{bundles,personas,laws,skills,workflows}.go`
> — deferred to a follow-up that rewires services to call providers
> directly."

What was actually load-bearing:

- Without the constructor rewire, services kept asking the Store for
  config. The Store kept its `InMemoryProviders` field. Per-project
  isolation across #112–#116 worked only for the closures
  (`SetTemplateCatalog`, `SetSynonyms`, …) — every other surface
  (workflow, bucket-by-key, guards, personas, skills, laws, settings)
  read from the shared singleton.
- Tasks #112–#116 inherited the violation. The closures for synonyms /
  stopwords were per-project because they were assigned per-Service;
  every other read still resolved through `Store.Providers()` and saw
  the last imported bundle regardless of which project's call
  dispatched it.

What #117 fixed:

- Snapshot type lives in `internal/config` as an immutable value-typed
  view.
- App services receive `*Snapshot` at construction (or fetch
  per-call) — captured *before* the dispatch happens.
- Store loses `Providers()`, `previousProviders`, `providersImported`,
  `providersMu`, `Providers()`, `PreviousProviders()`, `Snapshot()`,
  `PreviousSnapshot()`, and `ImportBundle`. `ProjectRuntime` owns the
  snapshot pair; the generic `Store.RecordEntityEvent(...,
  EventTypeBundleImported, ...)` helper remains as the audit-only
  surface for hooks subscribed to bundle.imported.
- BundleCache produces a fresh Snapshot per Reload; the new entry's
  `PreviousSnapshot` pins the prior pointer for the orphan flow.
- New tests pin per-project isolation, hot-reload safety, and
  concurrent dispatch under `-race`.

Lesson encoded by this task: **in-memory and hexagonal are not
parallel features — they are coupled.** Dropping SQL tables without
moving the methods *out of the Store* yields config in-memory inside
a SQL adapter — a hybrid with neither invariant's benefit. The actual
win (per-project isolation, concurrency safety) only materialises when
both invariants hold together.

## Appendix — #117 drift-closed addendum

A self-review after the initial close discovered seven additional
deviations from the spec letter. They are listed here so the next
reader can trace what was fixed and why each fix was structurally
forced (not "cosmetic"):

1. **GuardEvaluator extracted into `internal/app/guards/`.** Spec
   commit 2 named the package and the `NewGuardEvaluator(snap, repo,
   events)` constructor. The transition / operation guard logic
   moved wholesale out of `WorkflowService`; the service composes
   the evaluator and forwards facade methods
   (`EmitGuardViolated`, `EvaluateOperationGuards`) so existing
   callers (TaskService, CommentService, TUI render paths) keep
   their call shape.
2. **`NewTemplateService` captures `*config.Snapshot`.** Spec
   commit 3 lists `template_service` alongside the other
   Snapshot-consuming services. The field is currently unused at
   the write sites but keeps the API symmetric for future read-back
   paths.
3. **`NotificationService` consumes `Snapshot`.** Spec commit 3
   also names `notification_service`. The runtime composition root
   used to read `bundle.Notifications` directly; routing the read
   through the new service closes the last `bundle.*` read in the
   hot wiring path and keeps notification rotation in lockstep with
   `cache.Reload`.
4. **`NewOrphanService` takes `*config.Snapshot` directly.** Spec
   commit 3 mandates the concrete type rather than the looser
   `domain.BucketResolver` interface. Snapshot already satisfies
   the resolver shape, so the repository contract was unchanged.
5. **`SetPreviousSnapshot` dropped.** Spec commit 5 lists exactly
   one snapshot setter on `agent.Service`. The previous snapshot
   belongs on `ProjectRuntime`, not on the service. The runtime
   composition root now builds the OrphanService with both snapshot
   pointers and injects it via `SetOrphanService` — one setter
   swapped for another, but the new shape carries a fully-wired app
   service (the pattern spec commit 5 called "builders pass it to
   app services") instead of a raw state pointer.
6. **`Repositories.Snapshot` test override removed.** Spec commit 8
   routes every per-project view through `rt.Snapshot()`. The TUI
   field was a test-only escape hatch — the precise pattern the
   drift retrospective called out as load-bearing. Tests now wire a
   real `BundleCache` via `testfixtures/runtimecache.Install`;
   `rotateSnapshotAfterEdit` falls back to `cache.Install` when
   `cache.Reload` cannot run (test caches built without
   store/configstore/bus) so the cache remains the single source of
   truth in tests too.
7. **`internal/sqlite/workflows.go` ≤ 80 LOC.** Spec acceptance
   criterion. The two pure bucket-key/id resolver helpers that
   touched no state moved into `internal/sqlite/bucket_resolver.go`;
   workflows.go drops to 55 LOC carrying only state primitives.

Net surface change: zero new public setters (one added, one
deleted), zero new public types beyond the spec-named `guards`
package and `NotificationService`. Every `bundle.*` read in the
runtime composition root now flows through the Snapshot pointer.

## Appendix — Round-2 drift-closed

A second self-review (review round 2) flagged three remaining
violations of the spec letter on `SetXxx` methods that survived the
first close as "test escape hatches" — the exact rationale the #110
retrospective called out as load-bearing. Round-2 deleted them and
folds the closures back into `SetSnapshot`:

1. **`agent.Service.SetSynonyms` / `SetStopwords` / `SetRegistry`
   deleted.** Spec commit 5 listed all three for deletion under the
   "tudo deriva de Snapshot" rule. `Snapshot` now carries a
   pre-built `*domain.EnumRegistry` (built once at `BuildSnapshot`
   from the bundle's priority + severity tables). `SetSnapshot`
   derives the registry along with the catalog closures, synonym
   table, and stopword set — production composition collapses to a
   single call.
2. **`app.WorkflowService.SetRegistry` deleted.** Hot-reload no
   longer mutates the long-lived service through a setter; the cache
   rebuild produces a fresh `*WorkflowService` bound to the rotated
   Snapshot (stored on `ProjectRuntime.Workflow`) and `reloadBundle`
   swaps the TUI pointer at the same moment the rest of the
   snapshot-derived state rotates.
3. **`CommentService` / `TagService` / `ErrorService.SetSynonyms`
   deleted.** Constructors take `*config.Snapshot` directly; the
   services read `snap.Synonyms()` lazily. Two projects holding two
   snapshots get two synonym tables without any post-construction
   plumbing.

A fourth deviation — `TestConcurrentAgentsDifferentProjects`
exercising only snapshot map reads rather than real
`WorkflowService.MoveTask` cross-project — was closed by a new test
in `internal/app/guard_isolation_test.go`. Two snapshots over the
same workflow shape diverge on the guard list (A carries a
`comments_min` guard with count=99 that always trips; B carries
none); 64 goroutines per project run `MoveTask` in parallel under
`-race` and assert that every A call fails with `ErrGuardViolation`
while every B call succeeds. If A's guard list bled into B's
evaluator, every B call would also fail — the assertion would catch
the leak before the race detector even ran.

Net Round-2 surface change: three public setters deleted (zero
added), one new constructor parameter (`*config.Snapshot`) on three
already-snap-adjacent services, one new field on `ProjectRuntime`
(`Workflow`). No production caller threads any setter outside the
single `SetSnapshot` entry point.

## Appendix — Round-2 / W11 shared helpers

Symbols extracted during Round-2 + W11 to close the "same algorithm
inlined N times" drift the retrospective flagged. Each entry pins the
canonical site so future work extends the helper instead of forking a
new copy.

- **`txMutateAndEmit[T]`** (`internal/sqlite/txevent.go:100`). Generic
  "open tx → mutate → marshal payload → insert event row →
  commit → publish" pipeline. The post-commit publish keeps the
  subscriber-observed-event ⇒ row-on-disk invariant. 15 callsites
  across `comments.go`, `plans.go`, `tasks.go` route every write +
  audit-event pair through this helper.
- **`cursorwindow`** (`internal/tui/components/cursorwindow/`).
  Canonical cursor + scroll holder for fixed-row TUI surfaces whose
  chrome is owned by the parent renderer. Cursor / scroll fields are
  unexported; every mutation routes through a typed method
  (`MoveCursor`, `JumpFirst`, `PageDown`, `WithItemCount`,
  `WithViewport`) that re-runs the resync invariant. Three TUI uses:
  `graphCursor`, `plansCursor`, `planNetworkCursor`
  (`internal/tui/state.go:537,546,560`).
- **`sqlutil`** (`internal/sqlite/sqlutil/`). Dependency-free helpers
  shared by the SQL adapters:
  - `NullStringOr(v, fallback)` (`null.go`) — null-coercion with an
    explicit fallback.
  - `ScanRow[T]` (`scan.go`) — single decode closure shared by
    `QueryRowContext` / `QueryContext` paths so paired `scanFoo` /
    `scanFooRows` helpers cannot drift.
  - `MapSQLiteError` → `*ConstraintError` (`constraint.go:85`) —
    classifies SQLITE_CONSTRAINT_* extended codes into a typed
    `ConstraintError{Violation, Table, Field, Cause}` while
    preserving `errors.Is` chains through `Unwrap`.
- **`LoadFromDir[T]`** (`internal/config/loadfromdir.go:81`). Walks
  `dir` and `dir/custom` for files matching `opts.Suffixes`, decodes
  via `opts.Decode`, and dedups on `opts.SlugOf` under a
  caller-chosen `CollisionPolicy` (`CollideOverwrite`,
  `CollideError`, `CollideKeepFirst`). Collapsed three inline dedup
  loops (entity_loader, language, notification_loader).

## Update when

- `internal/config/snapshot.go` gains a new accessor, a new derived field, or changes the immutability contract.
- `agentruntime.BundleCache` / `ProjectRuntime` adds a field or changes its rotation semantics.
- A new invariant or anti-pattern is added/dropped during a future refactor.
- Migrations 022+ change the SQL adapter scope (further table drops, FTS schema shifts).

## See also

- [system.md](system.md) — runtime config knobs each project resolves through Snapshot.
- [path-resolution.md](path-resolution.md) — ConfigRoot precedence and `.omakiten/` walk-up.
