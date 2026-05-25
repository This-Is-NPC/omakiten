# Developer Guide

This guide is for people working **on** Omakiten — building, testing, and releasing the project. End-user docs live in [README.md](../../README.md) and the other `.docs/` guides.

## Getting Started

### Prerequisites

- [mise-en-place](https://mise.jdx.dev/) — pins the Go toolchain, `golangci-lint`, and `govulncheck` at the exact versions the merge gate uses. `mise install` reads `.mise.toml` and provisions everything.
- [GitHub CLI (`gh`)](https://cli.github.com/) — `scripts/local-check.sh` calls `gh api` to post the merge-gate commit status; `gh auth login` once is enough.

### Clone and verify

```bash
git clone https://github.com/This-Is-NPC/omakiten
cd omakiten
mise install              # provisions Go 1.25.x, golangci-lint, govulncheck
git config core.hooksPath scripts/hooks   # wire pre-push merge gate
mise run check            # full verification: tests + lint + vuln + docs:check
```

`mise run check` is the gate every PR must pass — see [Merge gate](#merge-gate) for how the pre-push hook posts the result to GitHub.

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
| `docs:check` | Same binary with `--check` — exits non-zero on drift; the local merge gate runs this. |

### Install & local state

| Task | What it does |
|---|---|
| `install` | `build` → installs `bin/okt` to `$HOME/.local/bin/okt`, syncs `defaults/` into `$HOME/.config/omakiten`, then runs `okt setup --update` (the same bubbletea picker `curl\|bash` users get; honours every `OKT_*` env var). The `--update` flag is load-bearing — it force-refreshes shipped defaults so repeat runs pick up edits under `defaults/` instead of silently keeping the pre-install copy on disk. Finishes with `okt init` against the repo. |
| `install:mcp:claude` | `build` → wires the local `bin/okt` into Claude Code's MCP config (`~/.claude.json`). |
| `install:mcp:claude-desktop` | Same, for Claude Desktop. |
| `install:mcp:opencode` | Same, for OpenCode. |
| `uninstall` | Removes `~/.local/bin/okt` and the shell wrapper. **Does not** touch config or data. |
| `purge` | Wipes `~/.config/omakiten` and `~/.local/share/omakiten`. Use after `uninstall` for a fresh-machine simulation. |
| `dev:sync` | Mirrors `defaults/` into `dev_env/` (overwrites root, leaves `dev_env/custom/`). |
| `dev:install` | `dev:sync` + builds `bin/okt` and runs `okt setup --skip-wrapper --skip-harnesses` against the dev-env so the binary works against `OMAKITEN_HOME=dev_env` without touching real state. |
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
  app/guards/            per-project guard Evaluator (transitions + operations + permissions)
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
  config/                official presets (omakase / izakaya / kaiseki / shokunin)
  languages/             21 bundled CLI/TUI language packs (en / pt-br / jp / …)
  themes/, notifications/, skills/, laws/, personas/, templates/
migrations/              SQLite schema migrations (001 … 027; 027 scopes sub-task parents by project)
scripts/                 install / uninstall / wrapper helpers + tests
```

Architecture rules are enforced in two places — see [architecture.md](architecture.md) and [CONTRIBUTING.md § Architecture boundaries](../../CONTRIBUTING.md#architecture-boundaries-enforced) for the rules in plain English. Run `go test ./internal/arch/...` after structural changes.

### Composition roots and the BundleCache

Both `internal/cli/root.go` and `internal/agentruntime/runtime.go` reach the same shape: parse the bundle once to seed the events bus, then call `agentruntime.NewBundleCache(...).SetProjectSelector(...)` + `cache.Resolve(ctx, projectID, configPath)`. `BundleCache` builds and caches one `*ProjectRuntime` per project id; the `BuildProjectRuntime` helper inside `internal/agentruntime/cache.go` is the single inflation path so boot, MCP per-project routing, CLI subcommands, and the TUI hot-reload all produce identical runtimes — divergence between code paths was the regression Phase 3a was designed to prevent.

`ConfigService.Import` no longer writes SQL config tables (migration 020 dropped them) and no longer touches the SQL adapter at all (Phase 2-bis). The method reduces to LoadBundle + HashFile, returning `(bundle, hash, *domain.EnumRegistry)`; the composition root then calls `config.BuildSnapshot(bundle)` to materialise the per-project Snapshot and emits `bundle.imported` via `Store.RecordEntityEvent`. Anything that needs to react to a bundle change subscribes to `bundle.imported` on the in-process bus. See [configuration-guide.md § How config reads work at runtime](../configuration-guide.md#how-config-reads-work-at-runtime-in-memory-providers--per-project-cache) for the full data flow.

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

- **Commit format:** [Conventional Commits](https://www.conventionalcommits.org/) in English. One intent per commit. Details in [CONTRIBUTING.md](../../CONTRIBUTING.md#commit-standards).
- **Branch naming:** `feature/<short-name>` or `fix/<short-name>`, kebab-case.
- **CHANGELOG:** `CHANGELOG.md` is generated by release-please from Conventional Commit subjects. Do not add a manual Unreleased section in normal PRs; put richer release notes in the PR body or in the release-please PR before it merges.
- **Docs:** end-user behaviour lives under `.docs/<topic>-guide.md`. When you change something a guide describes, update it in the same PR.

## Merge gate

The project runs its merge gate locally instead of on GitHub Actions. The two former workflows (`ci.yml` and the `ci-docs.yml` companion) are gone; the gate is now `scripts/local-check.sh` driven by a tracked pre-push hook.

### How it works

1. `scripts/hooks/pre-push` fires for every `git push`. For each non-deletion ref, it invokes `scripts/local-check.sh --pre-push` with the pushed SHA.
2. In `--pre-push` mode the script runs `mise run check` (`test` + `lint` + `vuln` + `docs:check`) synchronously — a red check aborts the push.
3. On green, the script spawns a detached background poller (`setsid nohup …`) that waits up to 60 seconds for the SHA to be reachable on `origin` (`gh api repos/<slug>/commits/<sha>`), then posts the final `success` status via `gh api -X POST repos/<slug>/statuses/<sha>`. This indirection is required because `git push` uploads the commit *after* the hook returns; posting in the hook itself hits HTTP 422 (`No commit found for SHA`).
4. For manual reruns (e.g. when the hook was skipped or the background post timed out), invoke `scripts/local-check.sh` directly — the default foreground mode posts `pending` → `success`/`failure` against the already-pushed SHA.
5. `master` branch protection requires `local-check` to be `success` for the PR's HEAD SHA before the merge button enables.

### Enabling for a fresh clone

```bash
git config core.hooksPath scripts/hooks
gh auth status              # ensure gh CLI is authenticated
```

`core.hooksPath` is per-clone (not committed); set it once after cloning. Skipping it disables the gate locally, which means `git push` will hand off a SHA with no `local-check` status — branch protection will refuse to merge it until the script is rerun manually.

To intentionally skip the gate on a push (e.g. release-please bot, force-push of WIP to a personal branch):

```bash
OKT_SKIP_LOCAL_CHECK=1 git push
```

### Re-running by hand

If the hook was skipped, the background poll timed out, or you just want to refresh the status:

```bash
scripts/local-check.sh                              # full run for HEAD (SHA must be on origin)
scripts/local-check.sh --sha=<sha>                  # full run for a specific SHA
scripts/local-check.sh --post-only --state=success  # skip the check, just stamp the status
scripts/local-check.sh --dry-run                    # print API calls, do not POST
```

The script is idempotent: re-running on the same SHA simply overwrites the latest status of the `local-check` context.

### Branch protection setup

The maintainer applies the policy via `gh api`. To re-apply (e.g. after the repo is recreated):

```bash
gh api -X PUT repos/This-Is-NPC/omakiten/branches/master/protection \
  --input - <<'JSON'
{
  "required_status_checks": { "strict": true, "contexts": ["local-check"] },
  "enforce_admins": false,
  "required_pull_request_reviews": null,
  "restrictions": null
}
JSON
```

`enforce_admins=false` keeps an emergency override for the solo maintainer; flip to `true` if collaborators are added.

### Why no hosted CI

The only workflow that remains in `.github/workflows/` is `release.yml` (release-please + asset builds). The merge gate moved local for three reasons:

- `mise run check` already covers the same surface (build + vet via `go test`, race-tested unit tests, `golangci-lint`, `govulncheck`, docs drift) and runs in seconds on the maintainer's box instead of minutes on a hosted runner.
- The old `ci-docs.yml` companion existed only to satisfy the required `build-test` check on doc-only PRs. A locally-posted status removes the need for that workaround entirely.
- Solo maintainer: cross-platform matrix isn't a constraint today. If a second contributor or a Windows/macOS regression appears, restore a thin `ci.yml` matrix alongside the local gate.

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

### The interactive picker didn't appear in `mise run install`

Two possible causes:
1. One or more `OKT_*` env vars are set in your environment — each supplied value skips the matching setup input. The CLI/TUI language input is shared, so either `OKT_CLI_LANG` or `OKT_TUI_LANG` resolves it; add `OKT_AGENT_LANG`, `OKT_PRESET`, and `OKT_HARNESSES` to run `okt setup` headlessly.
2. You're in a non-interactive shell (no controlling terminal). `okt setup` falls back to the env-var contract and surfaces a `validation_error` if any input is still missing. To force the picker, run interactively or pre-supply the values.

### `golangci-lint` complains about an import that `go vet` accepts

The repo enforces hexagonal boundaries via `depguard` rules in `.golangci.yml` mirrored by `internal/arch/arch_test.go`. If you hit a `depguard` violation, the rule's `desc:` string explains the boundary — fix the direction of the import, don't add an exception.

## Where to go next

- [README.md](../../README.md) — the user-facing entry point.
- [architecture.md](architecture.md) — hexagonal layout and adapter rules.
- [requirements.md](requirements.md) — behavioural map of the implemented surface (curated by `/document`).
- [CONTRIBUTING.md](../../CONTRIBUTING.md) — the canonical contributor checklist (commit standards, project knowledge base, workflow updates).
