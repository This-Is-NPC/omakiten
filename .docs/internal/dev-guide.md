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

Every task is defined in `.mise.toml` at the repo root and delegates to executable scripts under `scripts/`. Aggregators compose those tasks. Run with `mise run <name>` (or just `mise <name>`).

### Build & verification

| Task | What it does |
|---|---|
| `fmt` | Formats Go sources under `internal/`, `cmd/`, `defaults/`, `rules/`, and `scripts/`; skips scratch files. |
| `build` | Builds `.tmp/build/okt` with the current `git describe` version baked in via `-ldflags`. |
| `release:dry-run` | Builds all six GoReleaser archives in a temporary directory, generates the manifest and SLSA v1 statement, signs them with an ephemeral local fixture key using `scripts/testdata/offline-signing-config.json`, and verifies every digest. The fixture config declares no external signing services; local verification uses the generated public key without a transparency log. Production signing and installer verification retain their keyless identity and transparency-log requirements. |
| `test` | Runs one full all-package test pass, writes coverage under `.tmp/coverage/`, then enforces the unrounded 78.0% statement floor through the fail-closed checker. |
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
| `install:mcp:claude` | `build` → wires the local `.tmp/build/okt` into Claude Code's MCP config (`~/.claude.json`). |
| `install:mcp:claude-desktop` | Same, for Claude Desktop. |
| `install:mcp:opencode` | Same, for OpenCode. |
| `uninstall` | Confirms before removing `~/.local/bin/okt` and the shell wrapper. **Does not** touch config or data. |
| `purge` | Confirms before deleting `~/.config/omakiten` and `~/.local/share/omakiten`. Use after `uninstall` for a fresh-machine simulation. |
| `dev:sync` | Mirrors `defaults/` into `dev_env/` (overwrites root, leaves `dev_env/custom/`). |
| `dev:install` | Confirms the reset of dev-only `custom/` overlays, then syncs defaults, builds `.tmp/build/okt`, and runs `okt setup --update --skip-wrapper --skip-harnesses`. Use `tui:bare` when custom overlays or seeded fixtures must survive. |
| `tui` | Runs `dev:install` inside its raw-terminal task, then opens the TUI against the synchronized `dev_env/config/omakase.yaml`; `tui:bare` skips installation and opens the preset named by `dev_env/config/.active` so seeded fixtures survive without repo-local config discovery. |
| `gallery` | Opens the dev-only TUI component gallery (`cmd/okt-gallery`) — one shared component at a time, against the shipped theme. Not part of `build`; nothing in the `okt` binary imports it. |
| `gallery:dump` | Renders every component variant to stdout with no TTY, so it pipes to a file and diffs across a refactor. |
| `mcp:prompts` | Resolves every `okt-*` MCP prompt against the dev-env bundle and prints the composed markdown — handy for previewing what an agent receives without an MCP client. Depends on `dev:sync`. |

### Selecting MCP harnesses non-interactively

`mise run install` shows the interactive prompt by default. To pre-select harnesses (e.g. when scripting a fresh dev box):

```bash
OKT_HARNESSES=claude-code,opencode mise run install
```

Accepted separators: comma, space, tab, newline. The same env var works for the curl|bash installer.

In headless contexts (CI, no `/dev/tty`) the prompt is skipped silently — no hang, exit 0.

## Project Layout

```text
cmd/                     entry points (okt, okt-docs-refresh, …)
internal/
  domain/                pure types (no adapter imports)
  app/                   application services, ports
  agent/                 protocol-neutral agent intent layer
  agentruntime/          composition root (DB, config, paths, BundleCache)
  agentsetup/            MCP harness writer (claude-code, claude-desktop, opencode, crush, github-copilot, codex, cursor)
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
scripts/                 install / uninstall / wrapper helpers + tests
```

Architecture rules are enforced in two places — see [architecture.md](architecture.md) and [CONTRIBUTING.md § Architecture boundaries](../../CONTRIBUTING.md#architecture-boundaries-enforced) for the rules in plain English. Run `go test ./internal/arch/...` after structural changes.

### Composition roots and the BundleCache

Both `internal/cli/root.go` and `internal/agentruntime/runtime.go` reach the same shape: parse the bundle once to seed the events bus, then call `agentruntime.NewBundleCache(...).SetProjectSelector(...)` + `cache.Resolve(ctx, projectID, configPath)`. `BundleCache` builds and caches one `*ProjectRuntime` per project id; the `BuildProjectRuntime` helper inside `internal/agentruntime/cache.go` is the single inflation path so boot, MCP per-project routing, CLI subcommands, and the TUI hot-reload all produce identical runtimes. Rebuilds validate an inactive candidate before commit; only then are Store settings, hooks, and consumer state published. A rejected candidate leaves the cache, Store, model, and event rows untouched, so it can be retried. A drain timeout is returned and the inactive replacement is not published.

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
go test -race -count=1 ./internal/agentsetup/...  # narrow when iterating
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
# pick agents in the prompt → okt mcp setup runs for each → done
```

This is the closest you get to reproducing what a curl|bash user experiences without spinning up a fresh VM.

### Iterate on the TUI without touching real state

```bash
mise run tui                           # uses OMAKITEN_HOME=dev_env
```

`tui` invokes `dev:install` before opening the raw-terminal UI; that nested task runs `dev:sync`, so changes under `defaults/` are picked up automatically without detaching the TUI from the controlling terminal.

### Look at one shared component in isolation

```bash
mise run gallery                          # browse and open components
mise run gallery:dump > /tmp/before.txt   # every component and state, no TTY
```

In the Components column, `j/k` moves and the frame previews whatever is selected
— seeing a component costs no keystroke. `enter` hands it the keys; `esc` gives
them back without clearing the preview.

Once the component has the keys, every key belongs to it — `j/k`, `pgup/pgdn`, and
in the multiline form literally every character, because there the keystroke is the
content. That is why nothing is a chord: `tab` walks the columns instead.

The columns read left to right — Components, Scenario, the frame, Properties —
and `tab` walks them in that order: Scenario → frame → each property → Scenario.
`enter` on a component lands on Scenario, because picking one is how an
inspection starts. The frame sits inside a fixed-width container, so shrinking it
never drags the Properties column left to chase it, and the columns beside it
stand as tall as the screen rather than as tall as the simulated frame. A **scenario** is a preset:
one pick puts the frame AND the component into a whole situation at once ("narrow,
stacks", "empty lane", "translated labels"). It SEEDS and does not lock — every
value it wrote stays editable underneath.

**Properties** carries the frame's own inputs — height, width, padding, alignment —
and then, under a rule, **Component Properties**: the component's real arguments.
`WithViewport`'s row budget, `gridtable`'s column widths, `lane`'s Header
text, `keyfooter`'s MaxPrimaries, every `Spec` field the arranger reads. Type over
a number or a string, `←/→` picks a choice. A geometry left on `auto` follows the
frame; pin it to watch what a wrong number does.

The gallery's own chrome is built from bubbles and lipgloss only — never from the
components it exhibits. A tool whose index is a `cardlist` stops running the moment
`cardlist` breaks, which is exactly when it is needed. `TestTheChromeDoesNotImportWhatItInspects` pins it.

A screen golden only ever captures components already composed into a screen. The
gallery renders each `internal/tui/components/...` package on its own, live: the
component holds real state, so navigating it exercises the same mutators a screen
would call. Dump before and after a refactor to diff what moved.

The first entries are the layout packages — `screenlayout`, `screenkit`, `layout`,
`scrollwindow`, `cursorwindow`. They have no look of their own, so what they render
is the numbers they resolved for the frame: the breakpoint the arranger chose, the
row budget each section got, the chrome it measured, the window it sliced. Narrowing
the frame on the `screenlayout` entry is the only place a breakpoint is visible
without running a whole screen.

The size boxes are an honest simulation rather than a drawing. These components take
their geometry as an argument — `WithViewport(rows)`, `View(lines, viewport, hint)`,
`Render(rows, widths, border)` — so handing them smaller numbers is exactly what a
smaller terminal does. Sizes clamp to what the pane can actually draw, and content
that overflows the frame is clipped and counted rather than allowed to push the
frame open.

Components with no `View` of their own (`scrollwindow`, `cursorwindow`, `screenlayout`,
`screenkit`, `layout`) are not in the gallery yet; they are state and arithmetic, and
showing them means rendering their resolved numbers rather than their output.

### Run a specific MCP harness's setup repeatedly

While iterating on `internal/agentsetup`:

```bash
mise run build
./.tmp/build/okt mcp setup --harness codex --dry-run     # preview
./.tmp/build/okt mcp setup --harness codex --force       # actually write
```

`--dry-run` and `--force` apply to every supported harness.

## Testing

| Where | Run with |
|---|---|
| Go unit + integration tests | `go test -race -count=1 ./...` (or `mise run test`) |
| Hexagonal boundary check | `go test ./internal/arch/...` |
| Setup selection and Bash/PowerShell wrapper round trips | `mise run test:installer` — includes LF/CRLF idempotence and preservation of unrelated profile whitespace. |
| Release archive/metadata fixture | `mise run release:dry-run` |

The installer tests use Go's standard `testing` package and execute the production Bash and PowerShell entry points against isolated user directories. Run `mise run test:installer` whenever you touch the installers.

Installer checksum coverage is split by runner availability. `internal/installscript` executes the `install.sh` tamper-abort and positive paths hermetically. When a real PowerShell/Windows runner is unavailable, `install.ps1` is covered only by `[assumption]`-grade Go parity tests that statically assert the same checksum trust-root and verify-before-extract/copy/PATH/exec ordering as `install.sh`; those tests do not prove Windows execution. Treat real `install.ps1` tamper-abort as an integration gate to run when `pwsh` or Windows CI is available.

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

### Golden fixtures

Rendered output (TUI views, footers, help strips, pretty-printed reports) is snapshotted to `testdata/<name>.golden` and asserted through the single harness in `internal/testutil/golden.go`:

```go
import "omakiten/internal/testutil"

func TestScreenGolden(t *testing.T) {
    testutil.Golden(t, "project.view.golden", ansi.Strip(screen.View(frame)))
}
```

`Golden(tb, name, got)` resolves `name` against the calling package's `testdata/` directory, compares byte-exact, and reports a mismatch with `Errorf` so a loop over a screen's fixtures surfaces every drift in one run. `GoldenNewlineTerminated` is the same assertion for a fixture that predates the harness and was saved with a final newline its subject does not emit. It has exactly one consumer left — `internal/sqlite/search_integrity_test.go`, for `search_integrity_mixed.golden`. The other two (`home`, `plannetwork`) were re-recorded at three geometries and moved to the strict `Golden`; the helper's own doc comment names what it would take to retire the last one.

**Regenerating a fixture — the one supported way:**

```bash
go test ./internal/tui/screens/project -update    # rewrites that package's fixtures
```

Refresh one package at a time. `go test ./... -update` fails on purpose: the flag is registered by the harness, so packages that hold no fixtures reject it and the run aborts before writing anything. A plain `go test ./...` leaves every fixture byte-identical — that property is what lets a refactor's golden diff be read as evidence that rendering did not change.

There is deliberately no second switch. The tree used to carry an `UPDATE_GOLDEN=1` environment variable alongside the flag; it was removed, not deprecated. An exported variable is inherited by every child process of the shell that set it, so a single stale `export` turns later runs into silent mass-regeneration — and a regenerated fixture is a green test, so nothing reports it. `internal/arch/golden_refresh_boundary_test.go` enforces the rule: any golden-bearing file under `internal/` that writes a file, reads an environment variable or registers a refresh flag fails the build unless it carries a recorded reason in that gate's allowlist.

#### Screen baselines

Every package under `internal/tui/screens/` records its characterization baseline through `screentest.Record` in a `goldens_test.go`, rather than looping over `testutil.Golden` by hand. A `screentest.Recording` names the fixture stem, builds the screen fresh, replays the keys that move it off its entry state, and declares an `Assert` on the state it protects; `Record` drives it through the real host cycle (`Build` + `LifecycleEnter`, then `Update` per key with the host deps re-bound between messages, then one paint) and captures it at 80x24, 120x40 and 200x50 as `<name>.<geometry>.view.golden`.

```go
func TestBoardGoldens(t *testing.T) { screentest.Record(t, boardRecordings()) }
```

The recorder is not a second harness — `testutil.Golden` remains the only writer, and refresh is still `go test ./internal/tui/screens/board -update`. What it centralises is the three properties that make a fixture evidence:

- **the `Assert` gates the write.** It runs on refresh runs too, and a failure withholds the bytes. Without that, a `-update` after a contract broke would rewrite the fixture from the broken state and the next plain run would pass.
- **every recording is painted twice** over two independent materialisations of its fixture, and the two must agree byte for byte. This is the host-independence proof: a path, a clock reading, an address or a map iteration order that reaches a view fails here rather than surfacing as a mystery diff in a later migration.
- **a recording with no `Assert` is rejected.** A fixture that states nothing about the state it holds can be refreshed into an empty screen without anything noticing.

Assert on state the screen exposes — a non-zero `Scroll()`, a cursor off its default cell, an open mode, a dirty candidate — not on rendered text, which is what the fixture already records. And record state worth keeping: a body long enough to scroll, content wide enough to wrap at 80 columns, a cursor off its default. An empty screen proves nothing about a migration.

Coverage is enforced as one aggregate all-package run. The checker compares the unrounded profile statement ratio against a 78.0% floor and fails closed for missing, empty, malformed, stale, missing-total, or below-floor evidence; it does not define per-package floors or exemptions. Focused checker fixtures cover canonical grammar, extra fields and garbage ranges, portable nanosecond staleness, multi-file roots, ratio boundaries, and every failure case. The named-file checker fixture task does not add a package or coverage denominator.

```bash
mise run test
mise run test:coverage-checker
```

## Conventions

- **Commit format:** [Conventional Commits](https://www.conventionalcommits.org/) in English. One intent per commit. Details in [CONTRIBUTING.md](../../CONTRIBUTING.md#commit-standards).
- **Branch naming:** `feature/<short-name>` or `fix/<short-name>`, kebab-case.
- **CHANGELOG:** `CHANGELOG.md` is generated by release-please from Conventional Commit subjects. Do not add a manual Unreleased section in normal PRs; put richer release notes in the PR body or in the release-please PR before it merges.
- **Docs:** end-user behaviour lives under `.docs/<topic>-guide.md`. When you change something a guide describes, update it in the same PR.

## Merge gate

The merge gate runs locally through `mise run local-check`, backed by
`scripts/local-check.sh` and the tracked pre-push hook.

### How it works

1. `scripts/hooks/pre-push` fires for every `git push`. For each non-deletion ref, it invokes `scripts/local-check.sh --pre-push` with the pushed SHA.
2. The script requires the pushed SHA to equal HEAD in a clean checkout, runs `mise run check` synchronously, and checks the checkout again before reporting. A failed check aborts the push even when GitHub authentication is unavailable.
3. On green, `scripts/post-check-status.sh` waits up to 60 seconds for the commit to become reachable on GitHub, then reports `success`. The hook starts it with `nohup`; its diagnostics live under `.tmp/local-check/`. Missing authentication leaves the remote status unset and emits a warning after the checks have run.
4. Manual reruns use `mise run local-check` to report `success` or `failure` for an already-pushed HEAD. There is no mode that stamps a successful status without running checks.
5. `master` branch protection requires `local-check` to be `success` for the PR's HEAD SHA before the merge button enables.

### Enabling for a fresh clone

```bash
git config core.hooksPath scripts/hooks
mise exec -- gh auth status  # ensure gh CLI is authenticated
```

`core.hooksPath` is per-clone (not committed); set it once after cloning. Skipping it disables the gate locally, which means `git push` will hand off a SHA with no `local-check` status — branch protection will refuse to merge it until the script is rerun manually.

To intentionally skip the gate on a push (e.g. release-please bot, force-push of WIP to a personal branch):

```bash
OKT_SKIP_LOCAL_CHECK=1 git push
```

### Re-running by hand

If the hook was skipped, the background poll timed out, or you just want to refresh the status:

```bash
mise run local-check                    # clean HEAD must already be on GitHub
mise run local-check --sha HEAD          # explicit commit must resolve to HEAD
mise run local-check --dry-run           # print planned operations
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

Do **not** tag releases manually; merge the release PR and let the workflow create a draft. GoReleaser v2.17.0 builds without publishing and exports its checkout SHA; the signing checkout must resolve the tag to that exact SHA before metadata is created. The only job with `id-token: write` signs the version-bound manifest and exact `checksums.txt` bytes with Cosign v3.1.1, creates multi-subject SLSA v1/in-toto provenance, and verifies the Fulcio issuer plus exact repository/workflow identity. A separate non-OIDC job accepts and publishes an exact 11-file allowlist only after rejecting missing or extra files, so a signing, provenance, or artifact-set failure has no unsigned publication path. All actions are commit-pinned; version comments beside their SHAs record the inspected upstream major/release.

Hermetic tests exercise genuine Sigstore cryptography through `sigstore-go`'s virtual CA and genuine offline Cosign blob plus DSSE-attestation signing/verification with an ephemeral local key, including a forged-payload rejection. `.github/workflows/assurance.yml` makes Cosign mandatory and runs the release/install assurance packages on Linux and macOS; Windows runs the native PowerShell parser/SemVer checks and genuine Cosign path. The production release workflow closes the keyless positive-path gap without committing a captured fixture: immediately after keyless signing, `scripts/release-installer-gate.sh` serves the fresh release files over loopback and executes both installers' production strict-verification functions against the real Fulcio/Rekor bundles. Either failure prevents staging and publication. Local tests still never fabricate keyless material or write to Fulcio/Rekor.

Before changing the certificate policy, run `mise run release:dry-run`, inspect the first live release certificate, and preserve these exact constants unless a reviewed workflow identity migration requires new values:

```text
issuer:   https://token.actions.githubusercontent.com
identity: https://github.com/This-Is-NPC/omakiten/.github/workflows/release.yml@refs/heads/master
```

The first signed release is the first release after `v0.30.0`. Treat `v0.30.0` and older as checksum-only; never claim that a later signature is original build provenance for a historical artifact.

## Troubleshooting

### `mise run install` succeeded but `okt --version` still shows the old version

`.tmp/build/okt` is installed into `$HOME/.local/bin/okt`, but PATH may resolve `okt` from somewhere else (a stale `go install ./cmd/okt` puts it in `$(go env GOPATH)/bin`). The install task prints a `WARN` when this happens:

```text
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

## See also

- [architecture.md](architecture.md) — codebase shape.
- [data-model.md](data-model.md) — current SQLite schema and operational data.
- [../mcp.md](../mcp.md) — agent surface contract.
