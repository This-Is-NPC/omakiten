# Architecture

## Tech Stack

| Layer | Technology | Version |
|-------|-----------|---------|
| Language | Go | 1.25.0 (`go.mod`); 1.25.10 toolchain pin (`.mise.toml`) |
| CLI Framework | Cobra | v1.10.2 (`go.mod`) |
| TUI Framework | Bubble Tea | v1.3.10 (`go.mod`) |
| Terminal Styling | Lipgloss | v1.1.1-0.20250404203927-76690c660834 (`go.mod`) |
| ANSI helpers | `charmbracelet/x/ansi` | v0.11.6 (`go.mod`) |
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
| `github.com/charmbracelet/lipgloss` | v1.1.1-0.20250404203927-76690c660834 | Terminal styling and layout |
| `github.com/charmbracelet/x/ansi` | v0.11.6 | ANSI escape utilities used by TUI rendering |
| `github.com/pkoukk/tiktoken-go` | v0.1.8 | OpenAI BPE token counting (`cl100k_base`) |
| `github.com/google/uuid` | v1.6.0 | UUID generation (indirect, used by activity layer) |
| `github.com/dustin/go-humanize` | v1.0.1 | Human-readable formatting in TUI |

## Project Structure

| Directory | Responsibility |
|-----------|----------------|
| `cmd/okt/` | Binary entrypoint (`main.go`); invokes `cli.NewRootCommand` |
| `internal/cli/` | Cobra command tree (CLI composition root); JSON I/O wiring; project resolution |
| `internal/tui/` | Bubble Tea TUI: hierarchical zones (Tasks / Stats / Settings) plus a multi-project Home sentinel; sub-menus per zone (`state.go:topID`, `subID`, `topOrder`, `subsByTop`) + reusable components under `components/` |
| `internal/app/` | Application services + ports; workflow policy, bundle editing, dependency sync, template defaulting, read-model fan-out |
| `internal/domain/` | Pure entities, value types, and coded errors |
| `internal/config/` | Canonical YAML bundle schema, frontmatter parsing, entity-file rendering, validators |
| `internal/configstore/` | Right-side adapter that wraps `internal/config` I/O behind the `app.BundleStore` and `app.EntityFileWriter` ports |
| `internal/sqlite/` | Right-side adapter: SQLite store, schema migration, all repository implementations, activity logs, events |
| `internal/agent/` | Protocol-neutral agent service (intents, DTOs, error mapping); imports only `internal/app`, `internal/domain`, `internal/project`, `internal/token` |
| `internal/agentruntime/` | Composition root for the agent: opens sqlite, configstore, imports bundle, wires `agent.Service` (`runtime.go`) |
| `internal/agentsetup/` | MCP harness setup writer for the six supported harnesses (`claude-code`, `claude-desktop`, `opencode`, `crush`, `github-copilot`, `codex`); canonical list in `setup.go::SupportedHarnesses` |
| `internal/mcp/` | MCP adapter: maps MCP tools/resources/prompts to `agent.Service` calls + JSON-RPC stdio server |
| `internal/project/` | Active-project resolver (`--project-id`, `--project`, CWD precedence) |
| `internal/output/` | JSON envelope formatting for machine-parseable CLI output |
| `internal/token/` | Token estimation (BPE + word-count fallback) for context budgeting |
| `internal/graph/` | Cycle detection for task dependency DAGs |
| `internal/paths/` | Cross-platform config/data path resolution (XDG + `$OMAKITEN_HOME`) |
| `internal/activity/` | Context-scoped observability: `activity.Track`, `WithRepository`, `WithSource` |
| `internal/arch/` | Architecture-boundary test (`arch_test.go`) |
| `internal/testfixtures/` | Shared test helper that loads `config.Bundle` values from per-package `testdata/*.yaml` so test inputs flow through the production parser; convention is documented in [`dev-guide.md` § Test fixtures](dev-guide.md#test-fixtures) |
| `defaults/` | Embedded default kit assets (laws, skills, personas, templates, themes, notifications, official preset yamls under `config/` — `omakase.yaml` is the canonical kit) |
| `migrations/` | Embedded SQL schema migrations (001–017; latest: `017_drop_priority_severity_defaults.sql` — rebuilds `tasks` and `laws` to drop the SQL `DEFAULT` on `priority_id` / `severity_id`, completing the "no canonical defaults in code" principle: every write must pass an explicit id resolved from the user's config) |
| `dev_env/` | Local TUI/dev runtime state (`mise tui`) |
| `.docs/` | Documentation, templates, personal notes |
| `.workflow/` | Per-task requirements/plans/summaries used by the assisted-workflow skills |

## Architectural Patterns

- **Hexagonal / Ports and Adapters**: `internal/app/ports.go` declares every repository port; right-side adapters (`internal/sqlite`, `internal/configstore`) implement them; left-side adapters (`internal/cli`, `internal/tui`, `internal/mcp`, `internal/agentruntime`) are composition roots that construct app services and inject the right-side adapters. The diagram and rules are documented in `internal/app/doc.go`.
- **Boundary enforcement**: `internal/arch/arch_test.go` parses every non-test file under `internal/` and asserts that domain has no outward deps, app does not import concrete adapters, and `sqlite`/`configstore` do not import each other or app. The same rules are mirrored under `depguard` in `.golangci.yml`.
- **CQRS-like split**: Canonical YAML files are the write-model source of truth; SQLite is read-only operational state. Phase 2 of the in-memory config refactor (migration 020) dropped every SQL config table (`workflows`, `workflow_buckets`, `workflow_transitions`, `personas`, `persona_skills`, `skills`, `laws`, `settings`, `config_bundles`); Phase 2-bis closed the gap by stripping every config-side method from the SQL adapter. `app.ConfigService.Import` reduces to `LoadBundle` + `HashFile` and returns a `*domain.EnumRegistry` alongside the bundle. The composition root (`agentruntime.buildProjectRuntime`) then calls `config.BuildSnapshot(bundle)` to materialise an immutable `*config.Snapshot` and emits `bundle.imported` via `Store.RecordEntityEvent` for audit. Reads (workflow shape, personas, skills, laws, templates, notifications, mcp_commands, settings, synonyms, stopwords) go through the per-project `*config.Snapshot` captured at construction by every app service (`internal/config/snapshot.go`, `internal/app/workflow_service.go`, `internal/app/persona_service.go`, `internal/app/skill_service.go`, `internal/app/law_service.go`, `internal/app/template_service.go`, `internal/app/notification_service.go`, `internal/app/context_service.go`, `internal/app/orphan_service.go`, `internal/app/guards/evaluator.go`). The SQL adapter touches `config.Bundle` / `config.Snapshot` in zero production files.
- **BundleCache + per-project ProjectRuntime (Phase 3)**: `agentruntime.BundleCache` keys a `*ProjectRuntime` per project id. Each `ProjectRuntime` aggregates one bundle's `*agent.Service`, `hooks.Engine`, `ActionRegistry`, `NotificationShowAction`, `EnumRegistry`, `NotificationSnapshot`, `TagSynonyms`, `Stopwords`, `SourcePath`, and `Mtime`. `BundleCache.Resolve(ctx, projectID, configPath)` returns a cached entry, rebuilds on mtime change, or builds the first entry on miss; `Reload` forces a rebuild and stops the previous engine. The same `BuildProjectRuntime` runs from every composition root (`agentruntime.Open`, `cli/root.go` open, `tui/reload_bundle.go` Reload) so boot and reload cannot drift. MCP `Adapter.CallTool` peeks `project` / `project_id` from incoming args and dispatches against the `ServiceResolver`'s reply; the static default is shielded behind `DefaultServiceProvider` so a cache rebuild does not leave the adapter holding a stale `*agent.Service` (`internal/agentruntime/cache.go`, `internal/agentruntime/runtime.go`, `internal/mcp/adapter.go`). Hooks engines filter dispatch by `engine.projectID` so two projects' bundles never cross-fire (`internal/hooks/engine.go matchesProject`); zero on either side opts out so engines built before the composition root resolves a project id (bootstrap window, tests) and system events (`bundle.swapped`, `hook.executed` for project 0) still reach every engine.
- **Transactional file editing**: `app.BundleEditor` snapshots the bundle, stages atomic writes, and rolls back on failure before SQLite re-import (`internal/app/bundle_editor.go`).
- **Workflow policy in app**: `app.WorkflowService` owns default-bucket resolution, transition allowance, guard evaluation, `task.completed` emission, and the per-bucket CRUD policy (`ResolveBucketPermissions`). The sqlite layer holds only persistence primitives (`internal/app/workflow_service.go`).
- **Data-driven CRUD policy**: per-bucket and workflow-level overrides for `task.{edit,delete}` / `comment.{edit,delete}` are declared in YAML and serialized to SQLite as `permissions_json` (per bucket) and `defaults_json` (per workflow). The resolver walks bucket → workflow defaults → implicit `true`; comment falls back to task at every layer. No hardcoded "first bucket is special" rule (`internal/domain/workflow.go:ResolveTaskPermission` / `ResolveCommentPermission`).
- **Configurable domain enums (id↔value tables)**: enums whose labels are user-facing (`config.priorities`, `config.severities`) are declared in YAML as `[{id, value, default?, color?}, …]` and stored in SQLite by id (`tasks.priority_id` after migration 015, `laws.severity_id` after 016, with the SQL `DEFAULT` dropped in 017). Code references the integer id only; renderers (TUI, CLI, MCP DTOs) resolve labels through an instance-scoped `*domain.EnumRegistry` returned by `app.ConfigService.Import` between `LoadBundle` (validate) and `ImportBundle` (write), and injected via constructor into every service that needs it (`TaskService`, `LawService`, `WorkflowService`, `TUIQueryService`, `ContextService`, agent `Service`). Domain types carry no `MarshalJSON` / `UnmarshalJSON` — wire format is the raw int id, and label projection happens at DTO boundaries (e.g. `agent.TaskSummary.Priority` is the resolved label string). Renaming a label is a YAML edit — no code change, no data migration. Validator enforces ascending id order so the SQL sort weight and TUI cycle stay aligned with declaration order.
- **No canonical defaults in code**: the kit YAML at `defaults/config/omakase.yaml` (embedded via `go:embed`, materialised on first run by `internal/configstore.EnsureDefaultFiles`) is the **single canonical source** for every tunable. Code carries no `Default*` constants and no `Canonical*` slices for the configurable fields. The validator (`internal/config/validator.go`) treats every canonical field as required — bundles missing `mcp.*`, `tui.token_badge.*`, `views.*`, `priorities`, `severities`, `template_defaults`, `sqlite.busy_timeout_ms`, `activity_log.*`, `solutions.*`, `events.default_recent_limit`, `search.stopwords`, or `tag_synonyms` fail loud with messages pointing back at `defaults/config/omakase.yaml`. The `Effective*` accessors are identity passthroughs kept for explicit naming at call sites. A test-only helper (`internal/testfixtures.LoadBundle`) merges the embedded kit YAML on top of partial fixtures so test scenarios stay focused; production never falls back at runtime. Priority/severity resolution is fully dependency-injected via `*domain.EnumRegistry` (built from the validated bundle by `ConfigService.Import` and threaded through every service constructor — no process-global enum state remains). The two remaining process-global registries are tag synonyms in `internal/app` and stopwords in `internal/agent`: leaf helpers (`NormalizeTagName`, `wordSet`) have no per-call context to thread a resolver through. Composition roots (`agentruntime/runtime.go`, `cli/root.go`) are the single point that writes those two registries from the validated bundle. Connection-level SQLite knobs (`config.sqlite.busy_timeout_ms`, `config.activity_log.{max_rows, max_age_days}`, `config.events.default_recent_limit`) flow into the live `sqlite.Store` via `Store.ApplyConfig` after `ConfigService.Import`; service-level knobs (`config.solutions.{default_top_limit, max_top_limit}`) flow through `agent.ServiceSettings` and `app.ErrorService.SetSolutionsDefaults`.
- **Archive lifecycle**: tasks carry a `state` column (`active|archived`); `Archive`/`Unarchive` bypass bucket policy and transition guards but respect `operations.{archive,unarchive}.guards` (`internal/app/task_service.go`, `internal/sqlite/tasks_lifecycle.go`).
- **Coded errors**: Every domain error has a stable machine-readable code consumed by the JSON envelope and by the agent's recovery guidance (`internal/domain/errors.go`, `internal/agent/errors.go`).
- **Strict project scoping**: Operational rows are filtered by `project_id` at the repository layer (`internal/sqlite/tasks.go`, `internal/sqlite/comments.go`, `internal/sqlite/dependencies.go`, …).
- **Strict YAML validation**: `yaml.NewDecoder` uses `KnownFields(true)` to reject unknown fields and prevent silent drift (`internal/config/loader.go`, `internal/config/entity_loader.go`).
- **Protocol-neutral agent layer**: `internal/agent` knows nothing about MCP; `internal/mcp` is a thin translation layer; `internal/agentruntime` is the composition root.
- **Context-bound observability**: `activity.Track` uses context-scoped repositories so logging is opt-in per runtime and never breaks business logic (`internal/activity/track.go`, `internal/activity/context.go`).
- **Interactive notifications via CLI dispatch**: `config.Notification` cards accept an `Actions []NotificationAction` block (key/id/label/command). The notification component emits `ActionMsg{slug, action_id, command}` on a matching keystroke and the TUI Model runs `Command` in-process through `Repositories.DispatchCommand` (a closure built around `cli.NewRootCommand` so the same cobra root that serves the CLI also serves the prompt). The hooks engine pre-renders each `command[]` element through `text/template` against the triggering event's payload at NotificationShowMsg emit time so dispatch is dependency-free at key-press time. Every non-empty Command emits `confirmation.granted` keyed by ctx `author_type` (`human` from TUI, `agent` from any future MCP-driven surface) before the cobra invocation runs (`internal/config/notification.go`, `internal/tui/components/notification/notification.go`, `internal/tui/notification_action.go`, `internal/hooks/actions/notification.go`). Validator blocks `tui` and `mcp` as the first command token — re-entering those surfaces from a hook deadlocks the live program.
- **Hot-reload of the active config bundle**: the TUI Settings → Config picker re-imports a chosen preset in place through `Model.reloadBundle` — Phase 3e routed it through `Repositories.Cache.Reload(ctx, ProjectID, path)` (Phase 3e dropped the `ConfigService.Import` fallback, so cache-nil is a fast error rather than a silent SQL write). Reload returns a rotated `*ProjectRuntime`; reloadBundle reads its `Snapshot`, `Workflow`, and `EnumRegistry` (the raw `config.Bundle` is intentionally not exposed on the runtime — Invariant 1 keeps every consumer reading through `Snapshot`), repoints `BundleEditor.SetPath`, swaps `m.repos.Workflow` to the cache-built per-project `*app.WorkflowService` (Round-2 deleted `WorkflowService.SetRegistry` — the service is immutable post-construction so the cache rebuilds it bound to the rotated Snapshot), refreshes theme/styles/markdown/notifications/token-badge, and re-queries the task snapshot. `paths.SetActiveConfig` is written only after the rebuild succeeds so a validator rejection on the new bundle leaves the on-disk `.active` pointing at the previous (working) profile. A successful swap emits `bundle.swapped` with the orphan-preview payload; when orphans exist the model stashes the previous config path and the hook engine paints the `kitten_orphan_migration` prompt — esc on that prompt rebuilds against the previous bundle so the user is never left on a workflow they did not confirm (`internal/tui/reload_bundle.go`, `internal/tui/settings_picker.go`).
- **Orphan-task migration on workflow swap**: tasks pointing at a bucket whose workflow is no longer active are *orphans*. `app.OrphanRepository` exposes `PreviewOrphanedTasks` (read-only) and `RebindOrphanedTasks` (in-tx). The rebind matches each orphan to the same key in the new active workflow (preserved) or to the first active bucket (removed) and emits `task.migrated` per task with payload `{from, to, reason: "workflow_swap"}`. The TUI dispatches it through the in-process cobra command `okt workflow orphans --confirm`; CLI and MCP surfaces (`orphans.migrate` tool, two-phase confirmation) reach the same primitive (`internal/sqlite/orphans.go`, `internal/app/orphan_service.go`, `internal/cli/workflow.go`, `internal/agent/service_orphan.go`).

## Authentication

Not applicable — local-first single-user CLI/TUI/MCP tool with no authn or authz layer.

## Infrastructure

- **GitHub Actions workflows** (`.github/workflows/`):
  - `ci.yml` — triggered by `pull_request` against `master`. Runs `go build`, `go vet`, `go test -race -count=1`, and `golangci-lint` (v2.12.1) on Go 1.25.
  - `release.yml` — triggered by `pull_request: closed` against `master`. Runs `release-please-action` to manage Release PRs and tags; on a created release, runs `goreleaser-action` (v6, GoReleaser v2) to publish artifacts.
- **No Dockerfile / container orchestration**: distribution is via `goreleaser`-built binaries plus `install.sh` / `install.ps1`.
- **Local toolchain**: `.mise.toml` pins Go 1.25.9, `golangci-lint`, and `govulncheck`.
- **Local tasks** (`mise run <task>`): `fmt`, `test`, `build`, `install`, `dev:sync`, `tui`, `lint`, `vuln`, `check`, `install:mcp:claude`, `install:mcp:claude-desktop`, `install:mcp:opencode`, `uninstall`, `purge`.
- **Runtime data**: SQLite at `~/.local/share/omakiten/omakiten.db` (or `$XDG_DATA_HOME` / `$OMAKITEN_HOME/data`).
- **Runtime config**: `~/.config/omakiten/config/<active>.yaml` (basename recorded in `<config-dir>/.active`; `custom/<name>.yaml` shadows same-named root profiles; resolver falls through to discovery when the named profile is missing) plus per-entity `.md` files under sibling folders `skills/`, `laws/`, `personas/`, `themes/`, `templates/`, `notifications/`.
- **Release tooling**: `release-please-config.json`, `.release-please-manifest.json`, `.goreleaser.yml`, `install.sh` / `install.ps1`, automated by `.github/workflows/release.yml`.

## Code Metrics

| Metric | Status | Value / Finding | Source (tool + command) or Recommendation |
|--------|--------|-----------------|-------------------------------------------|
| Test structure | measured (2026-05) | 95 test files across 28 packages with tests; standard Go `testing`; table-driven tests driven by per-package `testdata/*.yaml` fixtures (loader in `internal/testfixtures` baseline-merges the embedded kit YAML so partial fixtures inherit canonical values, mirroring the install pipeline); map-based subtests + `t.Parallel()` adopted in new files (`CONTRIBUTING.md` §Patterns for new test files); golden snapshots under `internal/tui/testdata/*.golden`; native fuzz on parsers (`FuzzSplitFrontmatter`); integration-style CLI tests; TUI key-simulation tests; MCP adapter tests; agent service tests; agentsetup tests; activity log tests; domain priority + severity registry tests; dedicated boundary test in `internal/arch/` (hexagonal rules). All packages green at branch HEAD. | `go test ./...` |
| Test coverage | measured (2026-05) | Total **~67%**. Per-package floor 60% in non-exempt packages. Highlights: `internal/domain` 87.3%, `internal/tui/components/gridtable` 97.6%, `internal/tui/components/scrollwindow` 91.8%, `internal/cli` 66.5%. Exempt: `cmd/okt` (entry point) and `internal/testfixtures` (test helper). | `go test -coverprofile=/tmp/coverage.out ./... && go tool cover -func=/tmp/coverage.out` |
| Module sizes (LOC) | measured (2026-05) | 192 non-test files / 30,345 LOC; 95 test files / 19,205 LOC. Top 5 non-test: `internal/config/validator.go` (901), `internal/tui/render_task.go` (838), `internal/tui/model.go` (706), `internal/mcp/adapter.go` (703), `internal/config/bundle.go` (694). | `find . -name '*.go' ! -name '*_test.go' -exec wc -l {} +` |
| Cyclomatic complexity | recommended | Not measured. **Recommendation**: `gocyclo` — purpose-built for Go, configurable per-function threshold (e.g., 15). Install `go install github.com/fzipp/gocyclo/cmd/gocyclo@latest`; run `gocyclo -over 15 .`. | Tool: `gocyclo`; Rationale: Go-native, per-function reporting |
| Internal dependency structure | measured | No circular dependencies; hexagonal boundaries enforced by `internal/arch/arch_test.go` (passes) and mirrored as `depguard` rules in `.golangci.yml` (`golangci-lint run` → 0 issues). | `go list -deps ./...` per package + `go test ./internal/arch/...` |
| Mutation score | recommended | Not measured — no mutation testing configured. **Recommendation**: `gremlins` (Go-native mutation tester). Install `go install github.com/go-gremlins/gremlins@latest`; run `gremlins run`. Integrates with the existing `go test` suite. | Tool: `gremlins`; Rationale: Go-native, works with existing tests |
