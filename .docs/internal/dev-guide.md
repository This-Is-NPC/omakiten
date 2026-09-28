# Developer Guide

This guide is for people working **on** Omakiten — building, testing, and releasing the project. End-user docs live in [README.md](../../README.md) and the other `.docs/` guides.

## Contents

- [Getting Started](#getting-started)
- [Mise Tasks Reference](#mise-tasks-reference)
- [Project Layout](#project-layout)
- [Local Workflows](#local-workflows)
- [Testing](#testing)
- [Conventions](#conventions)
- [Merge gate](#merge-gate)
- [Releasing](#releasing)
- [Troubleshooting](#troubleshooting)
- [Where to go next](#where-to-go-next)
- [See also](#see-also)

## Getting Started

### Prerequisites

- [mise-en-place](https://mise.jdx.dev/) — pins the Go toolchain, `golangci-lint`, and `govulncheck` at the exact versions the merge gate uses. `mise install` reads `.mise.toml` and provisions everything.
- GoReleaser and Cosign are also pinned in `.mise.toml`; `mise install` provisions the exact release-dry-run versions.
- GitHub CLI, ShellCheck, and PowerShell are pinned in `.mise.toml`. `mise install` provisions them; authenticate GitHub CLI once with `mise exec -- gh auth login`. PowerShell 7.6.4 is the tested runtime; the 7.6.3 Linux runtime aborted during startup on the development host. An installed interpreter that cannot start fails the installer tests, and the mise test tasks require PowerShell instead of silently skipping it.

### Clone and verify

```bash
git clone https://github.com/This-Is-NPC/omakiten
cd omakiten
mise install              # provisions Go 1.25.13 (selected toolchain), golangci-lint, govulncheck
git config core.hooksPath scripts/hooks   # wire pre-push merge gate
mise run check            # full verification: tests + lint + vuln + docs:check
```

`mise run check` is the gate every PR must pass — see [Merge gate](#merge-gate) for how the pre-push hook posts the result to GitHub.

### First local install (optional)

To exercise the production-like binary against your real `~/.config/omakiten`:

```bash
mise run install          # builds, installs to ~/.local/bin/okt, runs the
                          # interactive skill-destination multi-select prompt
okt --version
okt tui
```

The local install task uses `~/.config/omakiten` for setup and project
registration, and registers with the selected active preset explicitly.
Repository-local configuration in a parent directory does not select the
installation profile. Normal CLI commands retain repository-local discovery.

Roll it back without touching project state:

```bash
mise run uninstall        # removes the binary + shell wrapper
mise run purge            # removes ~/.config/omakiten and ~/.local/share/omakiten
```

## Mise Tasks Reference

Every task is defined in `.mise.toml` at the repo root and delegates to executable scripts under `scripts/`. Aggregators compose those tasks. Run with `mise run <name>` (or just `mise <name>`).

### Build & verification

| Task | What it does |
|---|---|
| `fmt` | Formats Go sources under `internal/`, `cmd/`, `defaults/`, `rules/`, and `scripts/`; skips scratch files. |
| `build` | Builds `.tmp/build/okt` with the current `git describe` version baked in via `-ldflags`. |
| `release:dry-run` | Builds all six GoReleaser archives in a temporary directory, generates the manifest and SLSA v1 statement, signs them with an ephemeral local fixture key using `scripts/testdata/offline-signing-config.json`, and verifies every digest. The fixture config declares no external signing services; local verification uses the generated public key without a transparency log. Production signing and installer verification retain their keyless identity and transparency-log requirements. |
| `test` | Runs one full all-package test pass, enforces the 78.0% coverage floor using evidence under `.tmp/coverage/`, then runs concurrency and event-bus tests with the race detector. |
| `test:cross-build` | Compiles config, path, and SQLite safety tests for the six release targets, plus config/path tests for Plan 9, into `.tmp/tests/`. Each binary is named by package, OS, and architecture. |
| `test:profile <package> --bench <pattern>` | Profiles one package, placing its test binary and CPU/memory profiles under `.tmp/profiles/<import-path>/`. |
| `workspace:check` | Rejects misplaced generated files in the root and source directories, while allowing `.tmp/`, persistent dev state, tool configuration, and checked-in fixtures. |
| `scripts:check` | Checks Bash syntax and runs the pinned ShellCheck over every shell entry point and shared shell library, plus the public Bash installers. |
| `test:coverage-checker` | Runs focused coverage-profile grammar, freshness, and boundary fixtures. |
| `test:installer` | Runs setup selection, wrapper/uninstaller round trips, local-check regressions, and installer assurance through standard Go tests. |
| `lint` | `golangci-lint run` against `.golangci.yml`. |
| `vuln` | `govulncheck ./...`. |
| `check` | **PR gate.** Depends on `fmt:check`, `test`, `lint`, `vuln`, `docs:check`, `workspace:check`, and `scripts:check`. |
| `docs:refresh` | Runs `go run ./cmd/okt-docs-refresh --root .` to remove legacy generated-doc artifacts and validate that `.docs/` no longer carries old include/auto markers. |
| `docs:check` | Same binary with `--check` — exits non-zero on drift; the local merge gate runs this. |
| `language:new <code> <native> <name>` | Scaffolds a bundled pack from English, quotes header values, and adds translation TODO markers. Existing packs are never overwritten. |
| `local-check` | Checks a clean HEAD and reports its result to GitHub. `--sha` must resolve to HEAD; `--dry-run` prints planned operations. |
| `release:installer-gate <tag> <dist>` | Serves signed release files over loopback and executes both production installers' strict verification paths. |

The local build always runs because its version embeds Git tags, HEAD, and dirty
state; source-file timestamps alone do not establish that the binary is current.

### Install & local state

| Task | What it does |
|---|---|
| `install` | `build` → installs `.tmp/build/okt` to `$HOME/.local/bin/okt`, syncs `defaults/` into `$HOME/.config/omakiten`, then runs `okt setup --update` (the same bubbletea picker `curl\|bash` users get; honours every `OKT_*` env var). The `--update` flag is load-bearing — it force-refreshes shipped defaults so repeat runs pick up edits under `defaults/` instead of silently keeping the pre-install copy on disk. Finishes with `okt init` against the repo. |
| `uninstall` | Confirms before removing `~/.local/bin/okt` and the shell wrapper. **Does not** touch config or data. |
| `purge` | Confirms before deleting `~/.config/omakiten` and `~/.local/share/omakiten`. Use after `uninstall` for a fresh-machine simulation. |
| `dev:sync` | Mirrors `defaults/` into `dev_env/` (overwrites root, leaves `dev_env/custom/`). |
| `dev:install` | Confirms the reset of dev-only `custom/` overlays, then syncs defaults, builds `.tmp/build/okt`, and runs `okt setup --update --skip-wrapper --skip-harnesses`. Use `tui:bare` when custom overlays or seeded fixtures must survive. |
| `tui` | Runs `dev:install` inside its raw-terminal task, then opens the TUI against the synchronized `dev_env/config/omakase.yaml`; `tui:bare` skips installation and opens the preset named by `dev_env/config/.active` so seeded fixtures survive without repo-local config discovery. |
| `gallery` | Opens the dev-only TUI component gallery (`cmd/okt-gallery`) — one shared component at a time, against the shipped theme. Not part of `build`; nothing in the `okt` binary imports it. |
| `gallery:dump` | Renders every component variant to stdout with no TTY, so it pipes to a file and diffs across a refactor. |

## Project Layout

```text
cmd/                     entry points (okt, okt-docs-refresh, …)
internal/
  domain/                pure types (no adapter imports)
  app/                   application services, ports
  operation/             application operation facade
  contract/              shared delivery DTOs and ports
  terminal/              production TUI composition
  recovery/              filesystem snapshot and lease adapter
  updater/               release download and binary staging adapter
  commandcatalog/        canonical prompt command registry
  agentruntime/          composition root (DB, config, paths, BundleCache)
  cli/                   cobra commands (delegates to operation)
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
  output/                CLI/agent response envelopes
  paths/                 ConfigRoot / data-root resolver
  project/               active-project resolver
  testfixtures/          shared YAML-loader for tests
  token/                 token estimation
defaults/                ships into ~/.config/omakiten on first run
  config/                official presets (omakase / izakaya / kaiseki / shokunin)
  languages/             21 bundled CLI/TUI language packs (en / pt-br / jp / …)
  themes/, notifications/, skills/, laws/, personas/, templates/
scripts/                 install / uninstall / wrapper helpers + tests
```

Architecture rules are enforced in two places — see [architecture.md](architecture.md) and [CONTRIBUTING.md § Architecture boundaries](../../CONTRIBUTING.md#architecture-boundaries-enforced) for the rules in plain English. Run `go test ./internal/arch/...` after structural changes.

### Composition roots and the BundleCache

CLI and headless startup share `agentruntime.Bootstrap`, which creates the event
bus, configures the project selector and resolves the initial cache entry.
`buildProjectRuntime` is the cache's single inflation path. Rebuilds prepare an
inactive candidate, validate consumer acceptance, drain the previous engine,
then publish the replacement. A rejected candidate leaves the active runtime
unchanged. `BundleCache.Close` drains every cached project's engine before the
store closes. Project-resolution and reload errors propagate to callers.

The TUI receives `contract.RuntimeView` and operation ports. `internal/terminal`
connects those ports to the runtime; CLI code receives an injected interactive
runner. Notifications send structured operation intents through that binding.

`ConfigService.Import` loads and hashes the YAML bundle without writing SQL configuration rows. It returns `(bundle, hash, *domain.EnumRegistry)`; the composition root then calls `config.BuildSnapshot(bundle)` to materialise the per-project Snapshot and emits `bundle.imported` via `Store.RecordEntityEvent`. Anything that needs to react to a bundle change subscribes to `bundle.imported` on the in-process bus. See [configuration-guide/README.md § How config reads work at runtime](../configuration-guide/project-overrides.md) for the full data flow.

## Generated files and local state

`.tmp/` is the single workspace directory for disposable output. Its leading dot
keeps archived Go files out of `go test ./...` package discovery. Git ignores it.

| Path | Contents |
|---|---|
| `.tmp/build/` | Local binaries produced by `mise run build`. |
| `.tmp/coverage/` | Aggregate coverage profile and function summary from `mise run test`. |
| `.tmp/tests/` | Compiled test binaries, named by package and target. |
| `.tmp/profiles/` | Benchmark test binaries and CPU/memory profiles. |
| `.tmp/cache/` | Go build and golangci-lint caches configured by mise, plus local tool dependencies. |
| `.tmp/archive/` | Preserved local recovery copies and historical scratch notes. |

Persistent development configuration and databases stay under `dev_env/`; they
are not disposable build output. Tool-discovery files such as `opencode.json`
remain at the paths their tools require. Public installer scripts and release
configuration also retain their discovery paths.

The local OpenCode dependency directory is stored in `.tmp/cache/opencode/`
and linked from `.opencode/node_modules` so its plugin still resolves dependencies.

Use the mise tasks above instead of writing coverage or profiling output in the
root. For a focused manual compile, supply `-o` under `.tmp/tests/`. For a manual
profile, supply both `-o` and `-outputdir` under `.tmp/profiles/`: Go keeps a test
binary when CPU or memory profiling is enabled, even without `go test -c`.
Temporary downloads in CI use `RUNNER_TEMP`; release archives go under
`.tmp/release/`. Golden fixtures and the curated benchmark reference under
`.docs/internal/` are versioned evidence and remain beside their consumers.

Ephemeral compiler files, installer fixtures, and `testing.T.TempDir` use the
system temporary directory, honoring the caller's `TMPDIR`. Do not redirect
that directory onto the project filesystem: durable-write tests perform many
file and directory syncs, and moving them off a memory-backed temporary
filesystem can make the suite time out. Persistent reports and binaries still
use the `.tmp/` paths above.

Archive contents can include unique recovery data. Inspect them before deleting
anything; they are ignored by Git and cannot be recovered from a commit.

## Local Workflows

### Quick iteration loop

```bash
# edit code …
mise run test                                # fast feedback
mise run check                               # before committing

# compile filesystem safety tests for every release target plus Plan 9
mise run test:cross-build
mise run test:cross-build --target windows/arm64  # compile one target

# keep benchmark binaries and profiles out of the source tree
mise run test:profile ./internal/token --bench .
```

### Test the full installer flow locally

```bash
mise run uninstall && mise run purge   # clean slate
mise run install                       # build + install + interactive prompt

### Install the integration skill

`OKT_HARNESSES=agents,claude-code mise run install` publishes the embedded skill to the shared and Claude Code destinations. Use `agents` alone for the shared path or `0` to skip. `okt init --skill` publishes it in the registered project. See [Agent integration](../agents.md).
