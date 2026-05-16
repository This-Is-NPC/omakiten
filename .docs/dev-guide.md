# Developer Guide

This guide is for people working **on** Omakiten — building, testing, and releasing the project. End-user docs live in [README.md](../README.md) and the other `.docs/` guides.

## Getting Started

### Prerequisites

- [mise-en-place](https://mise.jdx.dev/) — pins the Go toolchain, `golangci-lint`, and `govulncheck` at the exact versions CI uses. `mise install` reads `.mise.toml` and provisions everything.

### Clone and verify

```bash
git clone https://github.com/This-Is-NPC/omakiten
cd omakiten
mise install              # provisions Go 1.25.x, golangci-lint, govulncheck
mise run check            # full verification: tests + lint + vuln
```

`mise run check` is the gate every PR must pass — green here means CI will go green too.

### First local install (optional)

To exercise the production-like binary against your real `~/.config/omakiten`:

```bash
mise run install          # builds, installs to ~/.local/bin/okt, runs the
                          # interactive MCP-harness multi-select prompt
okt --version
okt tui
```

Roll it back without touching project state:

```bash
mise run uninstall        # removes the binary + shell wrapper
mise run purge            # removes ~/.config/omakiten and ~/.local/share/omakiten
```

## Mise Tasks Reference

Every task is defined in `.mise.toml` at the repo root. Run with `mise run <name>` (or just `mise <name>`).

### Build & verification

| Task | What it does |
|---|---|
| `fmt` | `gofmt -w .` over the whole tree. |
| `build` | Builds `bin/okt` with the current `git describe` version baked in via `-ldflags`. |
| `test` | `go test ./...`. |
| `lint` | `golangci-lint run` against `.golangci.yml`. |
| `vuln` | `govulncheck ./...`. |
| `check` | **PR gate.** Depends on `test`, `lint`, `vuln`, `docs:check`. |
| `docs:refresh` | Runs `go run ./cmd/okt-docs-refresh --root .` to regenerate every embed-fed `.docs/_generated/` page from the live bundle. |
| `docs:check` | Same binary with `--check` — exits non-zero on drift; the CI gate runs this. |

### Install & local state

| Task | What it does |
|---|---|
| `install` | `build` → installs `bin/okt` to `$HOME/.local/bin/okt`, syncs `defaults/` into `$HOME/.config/omakiten`, runs `okt init` against the repo, installs the `okt()` shell wrapper, and runs the same interactive MCP-harness multi-select that `curl|bash` users get (via `scripts/harness-select.sh`). |
| `install:mcp:claude` | `build` → wires the local `bin/okt` into Claude Code's MCP config (`~/.claude.json`). |
| `install:mcp:claude-desktop` | Same, for Claude Desktop. |
| `install:mcp:opencode` | Same, for OpenCode. |
| `uninstall` | Removes `~/.local/bin/okt` and the shell wrapper. **Does not** touch config or data. |
| `purge` | Wipes `~/.config/omakiten` and `~/.local/share/omakiten`. Use after `uninstall` for a fresh-machine simulation. |
| `dev:sync` | Mirrors `defaults/` into `dev_env/` (overwrites root, leaves `dev_env/custom/`). |
| `dev:install` | `dev:sync` + builds `bin/okt` and runs `okt init` against the dev-env so the binary works against `OMAKITEN_HOME=dev_env` without touching real state. |
| `tui` | Runs the TUI against an isolated `dev_env/` (`OMAKITEN_HOME=dev_env`) — useful for trying changes without touching your real Omakiten state. Depends on `dev:install`. |
| `mcp:prompts` | Resolves every `okt-*` MCP prompt against the dev-env bundle and prints the composed markdown — handy for previewing what an agent receives without an MCP client. Depends on `dev:sync`. |

### Selecting MCP harnesses non-interactively

`mise run install` shows the interactive prompt by default. To pre-select harnesses (e.g. when scripting a fresh dev box):

```bash
OKT_HARNESSES=claude-code,opencode mise run install
```

Accepted separators: comma, space, tab, newline. The same env var works for the curl|bash installer.

In headless contexts (CI, no `/dev/tty`) the prompt is skipped silently — no hang, exit 0.

## Project Layout

```
cmd/                     entry points (okt, okt-docs-refresh, …)
internal/
  domain/                pure types (no adapter imports)
  app/                   application services, ports
  agent/                 protocol-neutral agent intent layer
  agentruntime/          composition root (DB, config, paths, BundleCache)
  agentsetup/            MCP harness writer (claude-code, claude-desktop, opencode, crush, github-copilot, codex)
  cli/                   cobra commands (delegates to app)
  mcp/                   MCP adapter (delegates to agent.Service)
  tui/                   bubbletea terminal UI
  sqlite/                sqlite-backed operational adapter (state only post-020)
  configstore/           filesystem-backed config adapter (bundle YAML + entity .md)
  config/                bundle types, loader, validator, snapshot, repo-local discovery
  activity/              context-bound tool-call tracker (events/operation rows)
  arch/                  hexagonal-boundary enforcement test
  events/                in-process event bus
  graph/                 dependency cycle/DAG helpers
  hooks/                 hooks engine + actions registry
  output/                CLI/MCP response envelopes
  paths/                 ConfigRoot / data-root resolver
  project/               active-project resolver
  testfixtures/          shared YAML-loader for tests
  token/                 token estimation
defaults/                ships into ~/.config/omakiten on first run
migrations/              SQLite schema migrations (001 … 021)
scripts/                 install / uninstall / wrapper helpers + tests
```

Architecture rules are enforced in two places — see [architecture.md](architecture.md) and [CONTRIBUTING.md § Architecture boundaries](../CONTRIBUTING.md#architecture-boundaries-enforced) for the rules in plain English. Run `go test ./internal/arch/...` after structural changes.

### Composition roots and the BundleCache

Both `internal/cli/root.go` and `internal/agentruntime/runtime.go` reach the same shape: parse the bundle once to seed the events bus, then call `agentruntime.NewBundleCache(...).SetProjectSelector(...)` + `cache.Resolve(ctx, projectID, configPath)`. `BundleCache` builds and caches one `*ProjectRuntime` per project id; the `BuildProjectRuntime` helper inside `internal/agentruntime/cache.go` is the single inflation path so boot, MCP per-project routing, CLI subcommands, and the TUI hot-reload all produce identical runtimes — divergence between code paths was the regression Phase 3a was designed to prevent.

`ConfigService.Import` no longer writes SQL config tables (migration 020 dropped them) and no longer touches the SQL adapter at all (Phase 2-bis). The method reduces to LoadBundle + HashFile, returning `(bundle, hash, *domain.EnumRegistry)`; the composition root then calls `config.BuildSnapshot(bundle)` to materialise the per-project Snapshot and emits `bundle.imported` via `Store.RecordEntityEvent`. Anything that needs to react to a bundle change subscribes to `bundle.imported` on the in-process bus. See [configuration-guide.md § How config reads work at runtime](configuration-guide.md#how-config-reads-work-at-runtime-in-memory-providers--per-project-cache) for the full data flow.

### Migration 020 / 021 — `tasks.bucket_id` rebind

Migration 020 dropped every SQL config table; before the drops it now rewrites `tasks.bucket_id` from the SQL-era `workflow_buckets.id` (autoincrement PK) to `workflow_buckets.local_id` (the YAML-declared id the post-migration `Snapshot` indexes by). Without that rewrite, tasks point at integers `Snapshot.BucketByID` cannot resolve and every view renders empty. Migration `021_rebind_orphan_buckets.sql` is the pure-SQL recovery for databases that applied the pre-rebind shape of 020: it walks the `events` table for each task's latest `task.moved` / `task.created` payload, extracts the bucket key, and maps onto the canonical preset id via a `CASE` covering every shipped preset key. Tasks with no recoverable event land in bucket id 1; users reorganise via TUI / CLI. Idempotent on fresh installs whose `bucket_id` is already in the canonical YAML range.


## Local Workflows

### Quick iteration loop

```bash
# edit code …
mise run test                                # fast feedback
go test -race -count=1 ./internal/agentsetup/...  # narrow when iterating
mise run check                               # before committing
```

### Test the full installer flow locally

```bash
mise run uninstall && mise run purge   # clean slate
mise run install                       # build + install + interactive prompt
# pick agents in the prompt → okt mcp setup runs for each → done
```

This is the closest you get to reproducing what a curl|bash user experiences without spinning up a fresh VM.

### Iterate on the TUI without touching real state

```bash
mise run tui                           # uses OMAKITEN_HOME=dev_env
```

`dev:sync` is a `depends` of `tui`, so changes under `defaults/` are picked up automatically.

### Run a specific MCP harness's setup repeatedly

While iterating on `internal/agentsetup`:

```bash
mise run build
./bin/okt mcp setup --harness codex --dry-run     # preview
./bin/okt mcp setup --harness codex --force       # actually write
```

`--dry-run` and `--force` apply to every supported harness.

## Testing

| Where | Run with |
|---|---|
| Go unit + integration tests | `go test -race -count=1 ./...` (or `mise run test`) |
| Hexagonal boundary check | `go test ./internal/arch/...` |
| `install.sh` shell-wrapper idempotency | `bash scripts/wrapper_idempotency_test.sh` |
| `install.sh` harness selection | `bash scripts/installer_select_test.sh` |
| `install.ps1` harness selection | `pwsh -NoProfile -File scripts/installer_select_test.ps1` |

The shell tests do **not** depend on Go; they extract helper functions from `install.sh` / `install.ps1` via awk / PowerShell AST and exercise them in-process. Run them whenever you touch the installers.

### Test fixtures

Tests construct `config.Bundle` values from real YAML files under each package's `testdata/` directory instead of inline Go literals. This keeps test inputs identical to what the parser sees in production from `defaults/config/omakase.yaml` — there is no "works in tests, fails in prod" drift.

The single loader entry point lives in `internal/testfixtures`:

```go
import "omakiten/internal/testfixtures"

func TestSomething(t *testing.T) {
    bundle := testfixtures.LoadBundle(t, "policy_comment_inherits_task.yaml")
    // ...
}
```

`LoadBundle(t, name)` reads `<package-dir>/testdata/<name>` relative to the calling test's package. `LoadBundleFromAbsPath(t, path)` covers the rare cross-package fixture. Both helpers terminate the test via `t.Fatalf` on parse/read failure — callers do not thread errors.

**Conventions:**
- One fixture per scenario; one scenario per file.
- Naming: `<feature>_<scenario>.yaml` (e.g. `policy_comment_inherits_task.yaml`). Avoid generic names — the filename documents the test.
- Every fixture begins with a YAML comment describing the scenario, expected resolver behavior, and what a passing test proves. If a test reads a fixture and the comment is wrong, fix the comment first — it is the source of truth.
- Add a new fixture when the policy shape differs; reuse an existing one when only the task or keystroke varies. Two near-identical files beat one with mental-overlay comments.

**Limitation:** `config.Bundle.{Skills,Personas,Laws}` carry `yaml:"-"` because production loads them from per-entity folders next to the YAML, not from the YAML itself. Tests that need those entities wire them in Go after `LoadBundle` returns — see `internal/app/context_service_test.go` for the canonical pattern.

Coverage target: don't drop below the current baseline.

```bash
go test -coverprofile=/tmp/coverage.out ./...
go tool cover -func=/tmp/coverage.out | tail -1
```

## Conventions

- **Commit format:** [Conventional Commits](https://www.conventionalcommits.org/) in English. One intent per commit. Details in [CONTRIBUTING.md](../CONTRIBUTING.md#commit-standards).
- **Branch naming:** `feature/<short-name>` or `fix/<short-name>`, kebab-case.
- **CHANGELOG:** notable user-visible changes go under `## Unreleased` in [CHANGELOG.md](../CHANGELOG.md). `release-please` rewrites the version headings on release — never edit them by hand.
- **Docs:** end-user behaviour lives under `.docs/<topic>-guide.md`. When you change something a guide describes, update it in the same PR.

## Continuous Integration

CI lives in `.github/workflows/`. Two workflows share the `name: CI` and the `build-test` job name so branch protection rules only ever need to require one check.

| File | Trigger paths | What it does |
|---|---|---|
| `ci.yml` | `paths-ignore: ["**.md", ".docs/**", "CHANGELOG.md"]` | Full Go pipeline — build, vet, race-tested `go test`, `golangci-lint`, `okt-docs-refresh --check`. |
| `ci-docs.yml` | `paths: ["**.md", ".docs/**", "CHANGELOG.md"]` | Always-pass companion. Posts the `build-test` check without spinning the Go toolchain for doc-only diffs. |

The two filters partition every PR diff:

- **Go-only diff** — only `ci.yml` runs; doc-companion is filtered out. One `build-test` check, real result.
- **Doc-only diff** — only `ci-docs.yml` runs; main is filtered out. One `build-test` check, always green, completes in seconds.
- **Mixed diff** — both run. Both post `build-test`; branch protection blocks unless both pass (i.e. the real Go pipeline must pass).

### Cancel-on-push

`ci.yml` declares:

```yaml
concurrency:
  group: ${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: true
```

A new push to the same PR branch automatically cancels any in-flight run. Stale runs on superseded SHAs no longer waste runner minutes or block the queue.

### Build cache

`ci.yml` manages the Go cache explicitly instead of relying on `setup-go@v5`'s built-in cache, so the build-output cache (`~/.cache/go-build`) is preserved alongside the module cache:

```yaml
- name: Set up Go
  uses: actions/setup-go@v5
  with:
    go-version: "1.25"
    check-latest: true
    cache: false             # managed below

- name: Cache Go build & module cache
  uses: actions/cache@v4
  with:
    path: |
      ~/.cache/go-build
      ~/go/pkg/mod
    key: ${{ runner.os }}-go-1.25-${{ hashFiles('go.sum') }}
    restore-keys: |
      ${{ runner.os }}-go-1.25-
```

The primary key invalidates whenever `go.sum` moves; the `restore-keys` fallback lets a cold key still hydrate from the most recent partial match, so an isolated dependency bump does not force a full rebuild from source. The Go build cache is itself content-addressed, so stale entries for changed source files are ignored automatically.

### Editing the workflows

When tightening or relaxing the doc-path filters in `ci.yml`, mirror the change in `ci-docs.yml` — the two `paths`/`paths-ignore` lists must remain exact complements. A drift means doc-only diffs would either skip CI entirely (no check posted, branch protection blocks) or trigger both workflows on the same files (duplicate green checks but no Go validation).

## Releasing

Releases are automated by [release-please](https://github.com/googleapis/release-please) (see `release-please-config.json` and `.release-please-manifest.json`). The release PR is generated from Conventional Commit messages on `master`:

- `feat:` / `feat(scope):` → minor bump.
- `fix:` / `fix(scope):` → patch bump.
- `feat!:` or `BREAKING CHANGE:` in the body → major bump.
- `chore:`, `refactor:`, `test:`, `docs:`, `ci:`, `build:`, `perf:` → no version bump (still appear in the changelog when relevant).

Do **not** tag releases manually; merge the release PR and let the workflow attach the GitHub release with the prebuilt binaries.

## Troubleshooting

### `mise run install` succeeded but `okt --version` still shows the old version

`bin/okt` is installed into `$HOME/.local/bin/okt`, but PATH may resolve `okt` from somewhere else (a stale `go install ./cmd/okt` puts it in `$(go env GOPATH)/bin`). The install task prints a `WARN` when this happens:

```
WARN: PATH resolves okt to /home/you/go/bin/okt, not /home/you/.local/bin/okt.
       Remove the stale copy or reorder PATH so $HOME/.local/bin wins.
```

Fix: delete the stale binary or reorder PATH so `$HOME/.local/bin` precedes `$GOPATH/bin`.

### The interactive prompt didn't appear in `mise run install`

Two possible causes:
1. `OKT_HARNESSES` is set in your environment — the env override skips the prompt and pre-selects whatever it names.
2. You're in a non-interactive shell (no controlling terminal). `scripts/harness-select.sh` skips silently in that case. To force it, run interactively or set `OKT_HARNESSES` explicitly.

### `golangci-lint` complains about an import that `go vet` accepts

The repo enforces hexagonal boundaries via `depguard` rules in `.golangci.yml` mirrored by `internal/arch/arch_test.go`. If you hit a `depguard` violation, the rule's `desc:` string explains the boundary — fix the direction of the import, don't add an exception.

## Where to go next

- [README.md](../README.md) — the user-facing entry point.
- [architecture.md](architecture.md) — hexagonal layout and adapter rules.
- [_generated/requirements.md](_generated/requirements.md) — behavioural map of the implemented surface (regenerated by `/document`).
- [CONTRIBUTING.md](../CONTRIBUTING.md) — the canonical contributor checklist (commit standards, project knowledge base, workflow updates).
