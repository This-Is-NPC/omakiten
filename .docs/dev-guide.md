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
| `check` | **PR gate.** Depends on `test`, `lint`, `vuln`. |

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
| `tui` | Runs the TUI against an isolated `dev_env/` (`OMAKITEN_HOME=dev_env`) — useful for trying changes without touching your real Omakiten state. |

### Selecting MCP harnesses non-interactively

`mise run install` shows the interactive prompt by default. To pre-select harnesses (e.g. when scripting a fresh dev box):

```bash
OKT_HARNESSES=claude-code,opencode mise run install
```

Accepted separators: comma, space, tab, newline. The same env var works for the curl|bash installer.

In headless contexts (CI, no `/dev/tty`) the prompt is skipped silently — no hang, exit 0.

## Project Layout

```
cmd/okt/                 main entry point
internal/
  domain/                pure types (no adapter imports)
  app/                   application services, ports
  agent/                 protocol-neutral agent intent layer
  agentruntime/          composition root (DB, config, paths)
  agentsetup/            MCP harness writer (claude, opencode, crush, copilot, codex)
  cli/                   cobra commands (delegates to app)
  mcp/                   MCP adapter (delegates to agent.Service)
  tui/                   bubbletea terminal UI
  sqlite/                sqlite-backed adapters
  configstore/           filesystem-backed config adapter
  arch/                  hexagonal-boundary enforcement test
defaults/                ships into ~/.config/omakiten on first run
migrations/              SQLite schema migrations
scripts/                 install / uninstall / wrapper helpers + tests
```

Architecture rules are enforced in two places — see [architecture.md](architecture.md) and [CONTRIBUTING.md § Architecture boundaries](../CONTRIBUTING.md#architecture-boundaries-enforced) for the rules in plain English. Run `go test ./internal/arch/...` after structural changes.

### Composition roots and the BundleCache

Both `internal/cli/root.go` and `internal/agentruntime/runtime.go` reach the same shape: parse the bundle once to seed the events bus, then call `agentruntime.NewBundleCache(...).SetProjectSelector(...)` + `cache.Resolve(ctx, projectID, configPath)`. `BundleCache` builds and caches one `*ProjectRuntime` per project id; the `BuildProjectRuntime` helper inside `internal/agentruntime/cache.go` is the single inflation path so boot, MCP per-project routing, CLI subcommands, and the TUI hot-reload all produce identical runtimes — divergence between code paths was the regression Phase 3a was designed to prevent.

`ConfigService.Import` no longer writes SQL config tables (migration 020 dropped them). The method LoadBundle → providers.Swap → audit-event. Anything that needs to react to a bundle change subscribes to `bundle.imported` on the in-process bus. See [configuration-guide.md § How config reads work at runtime](configuration-guide.md#how-config-reads-work-at-runtime-in-memory-providers--per-project-cache) for the full data flow.


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
