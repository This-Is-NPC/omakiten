# ClaimNext Agent Ceiling

How many agents can call `plans.claim_next` against one Omakiten database at the same time before claims start failing?

This doc holds the measurement protocol, the reproduction command, and the environment-qualified answer. The raw machine-readable result lives beside it in [`claim-next-agent-ceiling-reference.json`](claim-next-agent-ceiling-reference.json). The harness is `internal/mcp/claim_next_ceiling_bench_test.go`.

> **Status: historical manual reference; a full rerun is required.** The 2026-08-02 result was produced by a full manual run from source revision `a69613257999ca05b66ecc75a27842bb7635c104`. The freshness check now rejects it because the Go toolchain, source inputs, SQLite driver, PRAGMAs and pool setup changed. Ordinary MCP tests still validate the artifact's shape; they do not certify its ceiling for the current code. Do not advance the revision or replace results without running the full protocol.

The ceiling is a **correctness** number, not a latency number. Latency is recorded and reported, never used as a pass criterion — a slow claim is still a correct claim.

## What is being measured

`plans.claim_next` wraps a single `BEGIN IMMEDIATE` write transaction (`internal/sqlite/plans.go:ClaimNextPlanTask`). The transaction is atomic by construction, so the interesting question is not "can two agents claim the same task" — they cannot — but **at what fan-out does the surrounding stack stop delivering a claim at all**: connection-pool exhaustion, `PRAGMA busy_timeout` expiry on the write lock, or a tool-level error surfacing instead of a task.

The benchmark therefore models real agents, not goroutines sharing one handle:

- **One independent stack per caller.** Each of the `N` callers gets its own `sqlite.Store` → `agent.Service` → `mcp.Adapter`, opened once and held for the whole level. Nothing is shared above the file.
- **One file-backed WAL database.** All `N` stacks point at the same on-disk database, so they contend on the real write lock rather than on separate in-memory copies.
- **One seeded task per caller.** Every burst seeds exactly `N` claimable tasks, so a fully correct burst is `N` distinct claims and zero empties.
- **Synchronized start.** All `N` goroutines register ready, then block on a single channel close, so the burst is a genuine simultaneous arrival.
- **`data_version` pinned before measuring.** Each store performs its `DataVersion` probe during setup, so the lazily-pinned connection is already taken when the burst begins and the pool state under test matches a warm agent.

### The pass criterion

A level passes only if **every** oracle holds on **every** burst, warmups included:

1. Zero duplicate, missing, foreign, empty, transport, tool, or decode claim results.
2. Exactly one persisted `tasks.assigned_to` per seeded task, matching the caller that received it.
3. Exactly one `task.assigned` event per seeded task with a matching assignee payload, and no assignment event for a foreign task.
4. Unchanged task bucket / plan / wave / active state, and byte-identical plan and wave rows before and after.
5. `PRAGMA quick_check` returns `ok` after every fresh-database invocation.

Any single violation fails the whole level. There is no error budget.

## Protocol

| Stage | Levels | Per level |
|---|---|---|
| Coarse | 1, 2, 4, 8, 16, 32, 64, 128 | 5 fresh-database invocations × (3 warmup + 30 measured bursts) = **150 measured bursts** |
| Refine | binary search between the last passing and first failing coarse level | same 150 measured bursts |
| Boundary | the largest contiguous error-free level | 5 fresh-database invocations × (3 warmup + 100 measured bursts) = **500 measured bursts** |

The coarse sweep is fixed at powers of two through 128. Raise `OKT_CLAIM_BENCH_MAX` to keep doubling past 128 — worth doing on hardware where 128 still passes, since the ceiling would then only be reportable as `>=128`. `OKT_CLAIM_BENCH_LEVELS` replaces the sweep outright with an explicit list.

Every invocation gets a brand-new database file, so a level's verdict is never carried by a warm page cache from the level before it. Each burst cleans up its own seeded tasks and assignment events after the oracle runs, which is why a level's plan and wave fingerprints must come back unchanged.

If the 500-burst boundary run exposes a low-rate failure the 150-burst search missed, the harness treats that level as the new failing side, falls back to the highest measured passing level below it, refines again, and re-runs the boundary. The published ceiling is always a level that survived 500 bursts.

### Ceiling definition

The ceiling is the **largest contiguous error-free level**. If no level failed anywhere in the sweep, the result is reported as `>=N` rather than `N` — the harness cannot claim to have found a limit it never reached.

## Reproduction

```
OKT_CLAIM_BENCH_LEVELS=1,2,4,8,16,32,64,128 OKT_CLAIM_BENCH_WARMUPS=3 OKT_CLAIM_BENCH_BURSTS=30 OKT_CLAIM_BENCH_INVOCATIONS=5 OKT_CLAIM_BENCH_BOUNDARY_BURSTS=100 OKT_CLAIM_BENCH_OUT=.docs/internal/claim-next-agent-ceiling-reference.json go test ./internal/mcp -run '^$' -bench '^BenchmarkClaimNextAgentCeiling$' -benchtime=1x -count=1
```

That exact string is what `claimBenchmarkReproductionCommand` emits and what the committed JSON carries in `reproduction_command`, so the two can never drift — `TestClaimNextCeilingReproductionCommand` pins it.

Add `-timeout=12h` when you actually run it. The full protocol can exceed Go's default 10-minute test timeout, which would kill it mid-sweep.

`-benchtime=1x` is load-bearing: `BenchmarkClaimNextAgentCeiling` is a protocol runner, not a microbenchmark, and fails fast if Go asks it to iterate. Drop `OKT_CLAIM_BENCH_OUT` to run the protocol without overwriting the committed reference.

The JSON schema is versioned (`schema_version`) and validated by `validateClaimBenchmarkResult`; `TestClaimNextCeilingReference` re-validates the committed file on every `go test ./internal/mcp`, so a reference that drifts out of protocol shape breaks the build rather than rotting quietly.

### Fast freshness check

`mise run claim-benchmark:freshness` performs a manual, non-benchmark check. It validates the JSON, compares the recorded revision with the current claim transaction, MCP/agent path, pool setup, benchmark harness, default SQLite settings, and Go module files, then compares the current Go and SQLite metadata with the reference. It does not execute any contention bursts and therefore cannot produce or renew a ceiling.

The committed historical reference fails this check against the current tree. Preserve that failure until the full protocol produces a replacement. A passing check means only that no known invalidation input changed; it never substitutes for a rerun.

Run the full protocol and replace both the JSON and this results section after any of these triggers:

- the `ClaimNextPlanTask` transaction, claim selection, assignment event, MCP adapter, or agent service path changes;
- SQLite connection-pool sizing, pinning, PRAGMAs, driver/version, schema, or default SQLite configuration changes;
- the benchmark protocol, synchronization, seeding, cleanup, correctness oracle, or sample counts change;
- the Go toolchain changes; or the result is being claimed for different hardware, OS/architecture, storage, or `GOMAXPROCS`.

## Results

**Manual 2026-08-02 result: 87 simultaneous agents**, qualified on the environment below and subject to the status warning above. First failing level: 88.

| Field | Value |
|---|---|
| Machine | AMD Ryzen AI 5 340 w/ Radeon 840M, 12 logical CPUs, `GOMAXPROCS=12`, 46 GiB RAM |
| OS / toolchain | Linux 7.1.4-arch1-1, `go1.25.12`, `linux/amd64` |
| SQLite | 3.53.0 via `modernc.org/sqlite` v1.50.0 |
| PRAGMAs | `journal_mode=wal`, `foreign_keys=1`, `synchronous=1`, `busy_timeout=5000`, `cache_size=-1024`, `mmap_size=0`, `temp_store=0` |
| Pool per caller | 3 open / 2 idle connections; 1 pinned by the `data_version` probe, 2 ordinary slots left |
| Storage | file-backed WAL databases under `/tmp/BenchmarkClaimNextAgentCeiling2944140826/001` |
| Source revision | `a69613257999ca05b66ecc75a27842bb7635c104` |
| Run | Generated `2026-08-02T22:43:03Z`; 4 050 measured bursts; 289 050 claims attempted; 288 946 succeeded; 104 errored |

### Per-level verdict

`p50`/`p95`/`p99`/`max` are per-claim latency in milliseconds.

| Phase | Agents | Measured bursts | Claims | Errored | p50 | p95 | p99 | max | Claims/s | Verdict |
|---|---|---|---|---|---|---|---|---|---|---|
| coarse | 1 | 150 | 150 | 0 | 0.25 | 0.34 | 0.46 | 0.57 | 3661 | pass |
| coarse | 2 | 150 | 300 | 0 | 0.75 | 1.64 | 1.77 | 2.15 | 1305 | pass |
| coarse | 4 | 150 | 600 | 0 | 1.68 | 8.93 | 9.15 | 9.62 | 468 | pass |
| coarse | 8 | 150 | 1 200 | 0 | 8.99 | 79.63 | 79.93 | 80.30 | 113 | pass |
| coarse | 16 | 150 | 2 400 | 0 | 18.90 | 180.47 | 330.21 | 430.93 | 78 | pass |
| coarse | 32 | 150 | 4 800 | 0 | 34.63 | 430.69 | 730.59 | 1231.76 | 66 | pass |
| coarse | 64 | 150 | 9 600 | 0 | 54.89 | 431.19 | 750.25 | 1951.03 | 104 | pass |
| coarse | 128 | 150 | 19 200 | 87 | 109.03 | 1556.22 | 4154.83 | 5048.79 | 65 | **fail** |
| refined | 96 | 150 | 14 400 | 6 | 95.53 | 1157.26 | 3053.06 | 5028.52 | 64 | **fail** |
| refined | 80 | 150 | 12 000 | 0 | 75.73 | 748.06 | 1336.39 | 2751.01 | 76 | pass |
| refined | 88 | 150 | 13 200 | 0 | 80.99 | 952.24 | 2450.81 | 4548.68 | 67 | pass |
| refined | 92 | 150 | 13 800 | 1 | 94.99 | 1057.85 | 2547.71 | 4931.39 | 64 | **fail** |
| refined | 90 | 150 | 13 500 | 1 | 93.10 | 1047.75 | 2648.32 | 5027.53 | 67 | **fail** |
| refined | 89 | 150 | 13 350 | 0 | 95.27 | 947.36 | 1849.09 | 5026.95 | 71 | pass |
| boundary | 89 | 500 | 44 500 | 2 | 95.38 | 947.49 | 2054.64 | 5030.88 | 70 | **fail** |
| boundary | 88 | 500 | 44 000 | 7 | 100.27 | 1357.19 | 2654.02 | 5031.43 | 55 | **fail** |
| refined | 84 | 150 | 12 600 | 0 | 99.29 | 1256.30 | 2346.81 | 4932.16 | 55 | pass |
| refined | 86 | 150 | 12 900 | 0 | 92.87 | 1051.43 | 2251.15 | 4954.77 | 64 | pass |
| refined | 87 | 150 | 13 050 | 0 | 74.12 | 846.86 | 2149.96 | 4758.64 | 76 | pass |
| boundary | 87 | 500 | 43 500 | **0** | 74.11 | 849.22 | 2252.02 | 4954.81 | 75 | **pass** |

The published ceiling rests on that last row: 500 bursts across 5 fresh databases, 43 500 claims, zero errors of any class, and `PRAGMA quick_check` clean on all 5 databases. Allocation cost at that level was about 8.7 KiB and 205 allocations per claim.

### The boundary revalidation earned its keep

88 and 89 both passed the 150-burst search and then failed the 500-burst boundary, with 7 and 2 bad claims respectively. Without the longer boundary sample the published ceiling would have been 89, and it would have been wrong. Every fallback the harness makes is visible in the `levels` array: a failing `boundary` row followed by lower levels being refined.

### What actually breaks

Every single failure at every level was the same one:

```
tool: begin immediate: database is locked (5) (SQLITE_BUSY)
```

Max per-claim latency reaches 5027–5049 ms on every failing level except refined 92, whose maximum was 4931 ms, against a `busy_timeout` of 5000 ms. The observed failure is the busy timeout expiring while a claim waits its turn on the write lock — a **saturation** limit, not an atomicity failure.

That distinction is the headline finding:

- **Duplicate claims: 0. Foreign claims: 0. Empty claims: 0. State-oracle failures: 0. `quick_check` failures: 0.** At every level tested, up to and including 128 agents where 87 claims failed outright.
- The claim transaction never handed the same task to two agents, never assigned a task nobody asked for, never moved a bucket, and never corrupted the file.

The non-zero `missing_claims`, `assignment_oracle`, and `event_oracle` counts alongside each `tool` error are the same event counted downstream, not separate defects: a claim that never completed leaves its seeded task unclaimed, hence unassigned, hence without a `task.assigned` event. The counts match the `tool` count exactly at every failing level.

`BEGIN IMMEDIATE` does exactly what it promises. At 88 concurrent callers, the longer boundary run first shows the queue in front of it growing past `busy_timeout`, and excess agents get an honest error instead of a task. Since the pass criterion for this measurement is zero errored claims, that error is what sets the ceiling at 87.

### Reading the number

87 is a floor for the tuned scenario, not a hard wall, and the shape around it matters more than the digit:

- It is a **single-machine, single-database, all-agents-claiming-at-the-exact-same-instant** number. Real agents claim once and then work for minutes; the benchmark has every agent claim, finish, and immediately claim again with a synchronized start. It is not a general cap on sustained real-world fan-out.
- Throughput at measured passing levels from 16 agents onward ranged from 55 to 104 claims/second. Adding agents primarily buys queueing, not throughput.
- The ceiling is sensitive to `busy_timeout`. Raising `config.sqlite.busy_timeout_ms` trades claim latency for a higher ceiling; the failing claims were waiting, not failing on merit.
- Re-measure after any change to the pool shape, the claim transaction, or the SQLite PRAGMAs — and on different hardware. This number is qualified on the machine in the table above and nothing else.

## Where to learn more

- Harness and result schema: `internal/mcp/claim_next_ceiling_bench_test.go`.
- Claim transaction: `internal/sqlite/plans.go:ClaimNextPlanTask` — including the comment explaining why the winning connection is released before the post-commit read.
- Pool shape: `internal/sqlite/store.go` (`SetMaxOpenConns(3)` / `SetMaxIdleConns(2)`).
- `PRAGMA busy_timeout` and the other SQLite knobs: [`../configuration-guide/system.md` § `config.sqlite`](../configuration-guide/system.md#configsqlite).

## See also

- [`../workflow.md`](../workflow.md) § Plans — multi-agent fan-out — how many agents to put in a wave.
- [`../mcp.md`](../mcp.md) § Plans — the `plans.claim_next` tool contract.
- [`data-model.md`](data-model.md) — `tasks`, `plans`, `plan_waves`, and the unified `events` log the oracle reads.
