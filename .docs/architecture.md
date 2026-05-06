# Architecture

## Tech Stack

| Layer | Technology | Version |
|-------|-----------|---------|
| Language | Go | 1.25.0 (`go.mod`); 1.25.9 toolchain pin (`.mise.toml`) |
| CLI Framework | Cobra | v1.10.2 (`go.mod`) |
| TUI Framework | Bubble Tea | v1.3.10 (`go.mod`) |
| Terminal Styling | Lipgloss | v1.1.0 (`go.mod`) |
| ANSI helpers | `charmbracelet/x/ansi` | v0.10.1 (`go.mod`) |
| Database | SQLite (pure Go) | v1.50.0 — `modernc.org/sqlite` (`go.mod`) |
| YAML Parsing | `gopkg.in/yaml.v3` | v3.0.1 (`go.mod`) |
| Token Counting | `tiktoken-go` | v0.1.8 (`go.mod`) |
| Build / Task runner | mise | `.mise.toml` |
| Linter | golangci-lint v2 | `.mise.toml`, `.golangci.yml` |
| Vuln Scanner | govulncheck | `.mise.toml` |

## Dependencies

| Dependency | Version | Purpose |
|-----------|---------|---------|
| `github.com/spf13/cobra` | v1.10.2 | CLI command tree, flags, help generation |
| `gopkg.in/yaml.v3` | v3.0.1 | YAML parsing/generation for config bundles + frontmatter |
| `modernc.org/sqlite` | v1.50.0 | Pure-Go SQLite driver (no CGo) |
| `github.com/charmbracelet/bubbletea` | v1.3.10 | TUI framework (Elm-like model/update/view) |
| `github.com/charmbracelet/lipgloss` | v1.1.0 | Terminal styling and layout |
| `github.com/charmbracelet/x/ansi` | v0.10.1 | ANSI escape utilities used by TUI rendering |
| `github.com/pkoukk/tiktoken-go` | v0.1.8 | OpenAI BPE token counting (`cl100k_base`) |
| `github.com/google/uuid` | v1.6.0 | UUID generation (indirect, used by activity layer) |
| `github.com/dustin/go-humanize` | v1.0.1 | Human-readable formatting in TUI |

## Project Structure

| Directory | Responsibility |
|-----------|----------------|
| `cmd/okt/` | Binary entrypoint (`main.go`); invokes `cli.NewRootCommand` |
| `internal/cli/` | Cobra command tree (CLI composition root); JSON I/O wiring; project resolution |
| `internal/tui/` | Bubble Tea TUI: BOARD, TABLE, GRAPH, CONFIG, LOGS views (`state.go:viewNames`) + reusable components under `components/` |
| `internal/app/` | Application services + ports; workflow policy, bundle editing, dependency sync, template defaulting, read-model fan-out |
| `internal/domain/` | Pure entities, value types, and coded errors |
| `internal/config/` | Canonical YAML bundle schema, frontmatter parsing, entity-file rendering, validators |
| `internal/configstore/` | Right-side adapter that wraps `internal/config` I/O behind the `app.BundleStore` and `app.EntityFileWriter` ports |
| `internal/sqlite/` | Right-side adapter: SQLite store, schema migration, all repository implementations, activity logs, events |
| `internal/agent/` | Protocol-neutral agent service (intents, DTOs, error mapping); imports only `internal/app`, `internal/domain`, `internal/project`, `internal/token` |
| `internal/agentruntime/` | Composition root for the agent: opens sqlite, configstore, imports bundle, wires `agent.Service` (`runtime.go`) |
| `internal/agentsetup/` | MCP harness setup writer for `claude-code`, `claude-desktop`, `opencode` |
| `internal/mcp/` | MCP adapter: maps MCP tools/resources/prompts to `agent.Service` calls + JSON-RPC stdio server |
| `internal/project/` | Active-project resolver (`--project-id`, `--project`, CWD precedence) |
| `internal/output/` | JSON envelope formatting for machine-parseable CLI output |
| `internal/token/` | Token estimation (BPE + word-count fallback) for context budgeting |
| `internal/graph/` | Cycle detection for task dependency DAGs |
| `internal/paths/` | Cross-platform config/data path resolution (XDG + `$OMAKITEN_HOME`) |
| `internal/activity/` | Context-scoped observability: `activity.Track`, `WithRepository`, `WithSource` |
| `internal/arch/` | Architecture-boundary test (`arch_test.go`) |
| `defaults/` | Embedded default kit assets (laws, skills, personas, templates, themes, omakiten.yaml) |
| `migrations/` | Embedded SQL schema migrations (001–009) |
| `dev_env/` | Local TUI/dev runtime state (`mise tui`) |
| `.docs/` | Documentation, templates, personal notes |
| `.workflow/` | Per-task requirements/plans/summaries used by the assisted-workflow skills |

## Architectural Patterns

- **Hexagonal / Ports and Adapters**: `internal/app/ports.go` declares every repository port; right-side adapters (`internal/sqlite`, `internal/configstore`) implement them; left-side adapters (`internal/cli`, `internal/tui`, `internal/mcp`, `internal/agentruntime`) are composition roots that construct app services and inject the right-side adapters. The diagram and rules are documented in `internal/app/doc.go`.
- **Boundary enforcement**: `internal/arch/arch_test.go` parses every non-test file under `internal/` and asserts that domain has no outward deps, app does not import concrete adapters, and `sqlite`/`configstore` do not import each other or app. The same rules are mirrored under `depguard` in `.golangci.yml`.
- **CQRS-like split**: Canonical YAML files are the write-model source of truth; SQLite is the read-model materialization repopulated via `app.ConfigService.Import` → `Store.ImportBundle` (`internal/app/config_service.go`, `internal/sqlite/bundles.go`).
- **Transactional file editing**: `app.BundleEditor` snapshots the bundle, stages atomic writes, and rolls back on failure before SQLite re-import (`internal/app/bundle_editor.go`).
- **Workflow policy in app**: `app.WorkflowService` owns default-bucket resolution, transition allowance, guard evaluation, and `task.completed` emission. The sqlite layer holds only persistence primitives (`internal/app/workflow_service.go`).
- **Coded errors**: Every domain error has a stable machine-readable code consumed by the JSON envelope and by the agent's recovery guidance (`internal/domain/errors.go`, `internal/agent/errors.go`).
- **Strict project scoping**: Operational rows are filtered by `project_id` at the repository layer (`internal/sqlite/tasks.go`, `internal/sqlite/comments.go`, `internal/sqlite/dependencies.go`, …).
- **Strict YAML validation**: `yaml.NewDecoder` uses `KnownFields(true)` to reject unknown fields and prevent silent drift (`internal/config/loader.go`, `internal/config/entity_loader.go`).
- **Protocol-neutral agent layer**: `internal/agent` knows nothing about MCP; `internal/mcp` is a thin translation layer; `internal/agentruntime` is the composition root.
- **Context-bound observability**: `activity.Track` uses context-scoped repositories so logging is opt-in per runtime and never breaks business logic (`internal/activity/track.go`, `internal/activity/context.go`).

## Authentication

Not applicable — local-first single-user CLI/TUI/MCP tool with no authn or authz layer.

## Infrastructure

- **No CI/CD pipelines**: no `.github/workflows/`, no Dockerfile, no container orchestration.
- **Local toolchain**: `.mise.toml` pins Go 1.25.9, `golangci-lint`, and `govulncheck`.
- **Local tasks** (`mise run <task>`): `fmt`, `test`, `build`, `install`, `dev:sync`, `tui`, `lint`, `vuln`, `check`, `install:mcp:claude`, `install:mcp:claude-desktop`, `install:mcp:opencode`, `uninstall`, `purge`.
- **Runtime data**: SQLite at `~/.local/share/omakiten/omakiten.db` (or `$XDG_DATA_HOME` / `$OMAKITEN_HOME`).
- **Runtime config**: `~/.config/omakiten/omakiten.yaml` plus per-entity `.md` files under `skills/`, `laws/`, `personas/`, `themes/`, `templates/`.
- **Release tooling**: `release-please-config.json`, `.release-please-manifest.json`, `.goreleaser.yml`, `install.sh` / `install.ps1` (no automated workflow file present).

## Code Metrics

| Metric | Status | Value / Finding | Source (tool + command) or Recommendation |
|--------|--------|-----------------|-------------------------------------------|
| Test structure | measured | 62 test files across 21 packages with tests; standard Go `testing`; table-driven tests; integration-style CLI tests; TUI key-simulation tests; MCP adapter tests; agent service tests; agentsetup tests; activity log tests; dedicated boundary test in `internal/arch/`. 505 tests pass. | `go test ./...` |
| Test coverage | measured | 69.1% statement coverage across the tested packages | `go test -coverprofile=/tmp/coverage.out ./... && go tool cover -func=/tmp/coverage.out` |
| Module sizes (LOC) | measured | 148 non-test files / 20,839 LOC; 62 test files / 12,079 LOC. Top 5 non-test: `internal/tui/render_task.go` (686), `internal/tui/render_board.go` (504), `internal/config/validator.go` (469), `internal/config/migration.go` (439), `internal/tui/model.go` (421) | `find . -name '*.go' ! -name '*_test.go' -exec wc -l {} +` |
| Cyclomatic complexity | recommended | Not measured. **Recommendation**: `gocyclo` — purpose-built for Go, configurable per-function threshold (e.g., 15). Install `go install github.com/fzipp/gocyclo/cmd/gocyclo@latest`; run `gocyclo -over 15 .`. | Tool: `gocyclo`; Rationale: Go-native, per-function reporting |
| Internal dependency structure | measured | No circular dependencies; hexagonal boundaries enforced by `internal/arch/arch_test.go` (passes) and mirrored as `depguard` rules in `.golangci.yml` (`golangci-lint run` → 0 issues). | `go list -deps ./...` per package + `go test ./internal/arch/...` |
| Mutation score | recommended | Not measured — no mutation testing configured. **Recommendation**: `gremlins` (Go-native mutation tester). Install `go install github.com/go-gremlins/gremlins@latest`; run `gremlins run`. Integrates with the existing `go test` suite. | Tool: `gremlins`; Rationale: Go-native, works with existing tests |
