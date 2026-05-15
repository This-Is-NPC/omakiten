# Per-project Snapshot architecture

Authoritative reference for how Omakiten holds a project's configuration
in memory and serves it to every dispatch path. Captures the
architectural shape #117 (Phase 2-bis) settled on and the drift from
#110 that motivated it.

Audience: contributors editing the agent runtime, the SQL adapter, or
any service that reads workflow shape / catalogs / settings. Reading
this file before changing those paths prevents reverting accidentally
to the shared-singleton model the migration retired.

## TL;DR

- Each project gets its own immutable `*config.Snapshot` pointer, owned
  by `agentruntime.ProjectRuntime`.
- The Snapshot is built once per bundle import; mutation flows through
  `BundleCache` producing a fresh pointer on rebuild, not by Swap on the
  existing one.
- App services capture the pointer at construction (`SetSnapshot` on
  the agent service; `repo.Snapshot()` per call for the file-CRUD
  services). N projects in the cache ⇒ N independent snapshots ⇒ zero
  cross-project leakage on hot paths.
- The SQL adapter (`*sqlite.Store`) is back to "tasks, comments, events,
  tags, dependencies, errors, solutions, context_entries, activity_logs"
  — no config knowledge beyond a transitional `Snapshot()` accessor.

## Layer diagram

```
                  ┌──────────────────────────────┐
                  │     omakiten.yaml (disk)     │
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
            │   ├── HooksEngine                      │
            │   └── ActionRegistry, …                │
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

The migration enforces five invariants. Code reviews on this area
should reject changes that break any of them.

1. **Store = SQL adapter, period.** No `config.Bundle`,
   `config.Snapshot`, `config.Providers` field on the Store. The
   transitional `Store.Snapshot()` accessor returns the value last
   passed to `ImportBundle`; it lives there only until ImportBundle
   moves out of the Store.
2. **Snapshot is immutable.** No `Swap`, no `atomic.Pointer` embedded,
   no `Set*` mutator. Mutation produces a new pointer; the old one
   keeps serving in-flight callers.
3. **App services receive `*config.Snapshot` at construction.**
   `WorkflowService` captures it once and reads through `s.snap.X`.
   File-CRUD services (`PersonaService`, `SkillService`, `LawService`,
   `ContextService`) re-fetch via `s.repo.Snapshot()` per call because
   they themselves mutate the bundle on disk; the fresh fetch picks up
   the post-import snapshot the editor just installed.
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
shape: the interface narrows to the methods still in use (today:
`SnapshotSource` + `ImportBundle`), or disappears entirely once
ProjectRuntime takes over ImportBundle.

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
  `providersMu`, `Providers()`, `PreviousProviders()`. The transitional
  `Snapshot()` accessor stays only until `ProjectRuntime` takes over
  `ImportBundle`.
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
