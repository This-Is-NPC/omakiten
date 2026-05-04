# Architecture

## Tech Stack

| Layer | Technology | Version |
|-------|-----------|---------|
| Language | Go | 1.25.0 (`go.mod`) |
| CLI Framework | Cobra | v1.10.2 (`go.mod`) |
| TUI Framework | Bubble Tea | v1.3.10 (indirect, `go.mod`) |
| Terminal Styling | Lipgloss | v1.1.0 (indirect, `go.mod`) |
| Database | SQLite (pure Go) | v1.50.0 (`modernc.org/sqlite`, `go.mod`) |
| YAML Parsing | gopkg.in/yaml.v3 | v3.0.1 (`go.mod`) |
| Token Counting | tiktoken-go | v0.1.8 (`go.mod`) |
| Build Tool | mise | via `.mise.toml` |
| Linter | golangci-lint | via `.mise.toml` |
| Vuln Scanner | govulncheck | via `.mise.toml` |

## Dependencies

| Dependency | Version | Purpose |
|-----------|---------|---------|
| `github.com/spf13/cobra` | v1.10.2 | CLI command tree, flags, help generation |
| `gopkg.in/yaml.v3` | v3.0.1 | YAML parsing and generation for config bundles and frontmatter |
| `modernc.org/sqlite` | v1.50.0 | Pure-Go SQLite driver (no CGo) |
| `github.com/charmbracelet/bubbletea` | v1.3.10 | TUI framework (Elm-like model/update/view) |
| `github.com/charmbracelet/lipgloss` | v1.1.0 | Terminal styling and layout |
| `github.com/pkoukk/tiktoken-go` | v0.1.8 | OpenAI BPE token counting (`cl100k_base`) |
| `github.com/google/uuid` | v1.6.0 | UUID generation |
| `github.com/dustin/go-humanize` | v1.0.1 | Human-readable formatting |

## Project Structure

| Directory | Responsibility |
|-----------|----------------|
| `cmd/okt/` | Binary entrypoint (`main.go`) |
| `internal/cli/` | Cobra command tree, JSON I/O wiring, project resolution at CLI layer |
| `internal/tui/` | Bubble Tea TUI: kanban board, table view, dependency graph, config entity browser, activity logs |
| `internal/app/` | Application services enforcing business rules (validation, workflow transitions, cycle detection, transactional file edits) |
| `internal/domain/` | Pure domain entities and coded errors |
| `internal/config/` | Canonical YAML bundle loading/saving, frontmatter parsing, entity file generation, validation |
| `internal/sqlite/` | Global SQLite database access, schema migrations, operational repositories, activity logs |
| `internal/project/` | Resolves active project from flags or CWD |
| `internal/output/` | JSON envelope formatting for machine-parseable CLI output |
| `internal/token/` | Token estimation (BPE / word-count) for context budgeting |
| `internal/graph/` | Cycle detection for task dependency DAGs |
| `internal/paths/` | Cross-platform config/data path resolution (XDG + `$OMAKITEN_HOME`) |
| `internal/agent/` | Protocol-neutral agent intent layer; no MCP SDK or transport dependency (`internal/agent/service.go`, `internal/agent/runtime.go`) |
| `internal/mcp/` | MCP adapter: maps MCP tools/resources/prompts to `internal/agent` services (`internal/mcp/adapter.go`, `internal/mcp/server.go`) |
| `internal/agentsetup/` | MCP harness setup (currently `claude-desktop`, `opencode`); writes harness config atomically without overwriting other entries (`internal/agentsetup/setup.go`) |
| `internal/activity/` | Observability layer: tracks app service calls with context-scoped repositories (`internal/activity/track.go`, `internal/activity/context.go`) |
| `defaults/` | Embedded default kit assets (YAML, themes, skills, laws, personas) |
| `migrations/` | Embedded SQL migration scripts |
| `testdata/` | Test fixtures |
| `dev_env/` | Local development environment state |
| `.omakiten/` | Project-local omakiten config |
| `.workflow/` | Development workflow artifacts (plans, requirements, summaries) |
| `.docs/` | Documentation and templates |

## Architectural Patterns

- **Layered Architecture**: `cmd` → `cli/tui` → `app` → `domain` → `sqlite/config`
- **Ports and Adapters (Hexagonal)**: `internal/app/ports.go` defines repository interfaces (`ProjectRepository`, `ConfigRepository`, `TaskRepository`, `CommentRepository`, `DependencyRepository`, `ContextEntryRepository`); `sqlite.Store` implements all of them (`internal/sqlite/store.go`)
- **CQRS-like separation**: Canonical YAML is the write-model source of truth; SQLite is the read-model materialization (`internal/config/loader.go`, `internal/sqlite/store.go`)
- **Transactional File Editing**: `BundleEditor` uses snapshot journals and atomic writes to ensure on-disk consistency before SQLite re-import (`internal/app/bundle_editor.go`)
- **Coded Errors**: Every domain error has a stable machine-readable code for agent recovery (`internal/domain/errors.go`)
- **Project Scoping**: All operational data is strictly filtered by `project_id` at the repository layer (`internal/sqlite/store.go`)
- **Strict YAML Validation**: `yaml.NewDecoder` with `KnownFields(true)` prevents unknown field drift (`internal/config/loader.go:120`)
- **Protocol-Neutral Agent Layer**: `internal/agent` depends only on `internal/app` services and `internal/domain`; it has no MCP SDK, package manager, or transport dependency (`internal/agent/service.go`). The MCP adapter (`internal/mcp`) is a thin translation layer.
- **Context-Bound Observability**: `activity.Track` uses context-scoped repositories so logging is opt-in per runtime and never breaks business logic (`internal/activity/track.go`)

## Authentication

Not applicable — local-first single-user CLI tool with no authentication or authorization layer.

## Infrastructure

- **No CI/CD pipelines**: No `.github/workflows/`, no `Dockerfile`, no container orchestration.
- **Local tool management**: `mise` (`.mise.toml`) pins Go 1.25.9, `golangci-lint`, and `govulncheck`.
- **Local tasks**: `fmt`, `test`, `lint`, `vuln`, `check`, `tui` defined in `.mise.toml`.
- **Database**: Local SQLite file (`~/.local/share/omakiten/omakiten.db`).
- **Config**: Local YAML files (`~/.config/omakiten/omakiten.yaml` + per-entity `.md` files under `skills/`, `laws/`, `personas/`, `themes/`).

## Code Metrics

| Metric | Status | Value / Finding | Source (tool + command) or Recommendation |
|--------|--------|-----------------|-------------------------------------------|
| Test structure | measured | 34 test files across 15 packages with tests; standard Go `testing`; table-driven tests; integration-style CLI tests; TUI key simulation tests; MCP adapter tests; agent service tests; agentsetup tests; activity log tests | `go test ./...` |
| Test coverage | measured | 72.3% statement coverage (15 tested packages) | `go test -coverprofile=/tmp/coverage.out ./... && go tool cover -func=/tmp/coverage.out` |
| Module sizes (LOC) | measured | Top 5 non-test: `internal/tui/model.go` (2,487), `internal/tui/entity.go` (1,078), `internal/sqlite/store.go` (1,027), `internal/agent/service.go` (609), `internal/config/loader.go` (382); 66 non-test files (12,139 LOC); 34 test files (6,287 LOC) | `find . -name '*.go' ! -name '*_test.go' -exec wc -l {} +` |
| Cyclomatic complexity | recommended | Not measured. **Recommendation**: `gocyclo` — purpose-built for Go, reports functions exceeding a configurable threshold (e.g., 15). Install: `go install github.com/fzipp/gocyclo/cmd/gocyclo@latest`; run: `gocyclo -over 15 .` | Tool: `gocyclo`; Rationale: Go-native cyclomatic complexity analyzer with per-function reporting |
| Internal dependency structure | recommended | Not measured. **Recommendation**: `go list -deps ./...` shows dependency count per package; for circular dependency detection and visualization, use a small Go script using `golang.org/x/tools/go/packages`. No circular dependencies suspected in current flat `internal/` structure. | Tool: `go list -deps` + custom script; Rationale: Native Go tooling, no extra dependencies needed |
| Mutation score | recommended | Not measurable — no mutation testing configured. **Recommendation**: `gremlins` (Go mutation testing). Install: `go install github.com/go-gremlins/gremlins@latest`; run: `gremlins run`. Fits the stack because it is Go-native and integrates with standard `go test`. | Tool: `gremlins`; Rationale: Go-native mutation tester that works with existing test suite |
