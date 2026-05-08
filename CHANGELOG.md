# Changelog

## Unreleased

### Features

* **mcp harnesses — codex, crush, github-copilot:** `okt mcp setup --harness` now configures three new agents. `crush` writes `mcp.omakiten` to Crush's documented global JSON config (`~/.config/crush/crush.json` on Linux/macOS; `%LOCALAPPDATA%\crush\crush.json` on Windows). `github-copilot` writes `servers.omakiten` to VS Code Copilot Chat's user-scope `mcp.json` (`<UserConfigDir>/Code/User/mcp.json`) — the agent-mode surface, root key `servers` not `mcpServers`. `codex` writes `[mcp_servers.omakiten]` to `~/.codex/config.toml`; setup learned a per-harness codec so JSON harnesses keep their sorted-key normalization while Codex round-trips through `pelletier/go-toml/v2` without disturbing unrelated TOML tables.
* **interactive installer:** `install.sh` and `install.ps1` now finish with a numbered multi-select over every supported harness and run `okt mcp setup --harness X --force` for each pick — install + MCP wiring is one step. Reads from `/dev/tty` so the prompt works under `curl|bash` (where stdin is the pipe); skipped silently when no TTY is reachable so CI pipelines don't hang. `OKT_HARNESSES=claude-code,opencode` (comma/space/tab/newline separated) bypasses the prompt and pre-selects, including in non-interactive contexts. Per-harness setup failures fall through to a manual re-run hint instead of aborting the loop.
* **tui navigation polish (T3):** the per-project chrome now reads as a single navigation surface. `left` / `right` (and `h` / `l`) **never** switch zones — they are within-view bindings (Board lanes, Stats period picker) or no-ops everywhere else. The Home tile is folded into the top strip as `00 // HOME` next to the three zone tiles, separated by a faded `│` divider, so the affordance stays visible from any per-project view; the breadcrumb in the title row truncates long slugs to keep the layout intact at 80 cols. New `ctrl+o` (vim-style "older") pops a 16-entry session-only back-stack populated whenever the user makes an intentional zone/sub navigation (`tab` / `1`–`3` / `,` / `/` / `0` / `ctrl+h`). The footer is rebuilt around structured `footerToken` records — up to three primary actions per surface render in `hintAccent`, the rest stay in muted `hint`, and `?` is guaranteed to be the trailing token wherever help is reachable. Esc verbal across overlays standardised to `esc back`. `selectHomeProject` preserves the user's zone/sub when hopping between projects so Home reads as a project switcher rather than a session reset.
* **tui navigation content split (T2):** Stats and Settings now own the content that used to live on the cramped Config view. **Stats › General** absorbs the project-wide Totals (tasks / comments / context entries / tags) and Tokens (estimated / max + a `[BUDGET EXCEEDED]` badge) blocks beneath the per-model breakdown. **Settings** splits into six sub-menus — `general` (read-only runtime card: okt version, omakiten.yaml path, SQLite path, active workflow, active theme) plus one column per entity kind (`laws`, `personas`, `skills`, `templates`, `tags`). The horizontal 5-column grid is gone; each entity kind renders full-width on its own sub. Sub navigation uses `,` / `/` (the framework introduced in T1); `t` (theme picker) and `c` (config picker) remain accessible from every Settings sub. Help groups, footer hints, and `currentHelpTitles` were updated to mirror the new layout. The runtime / tokens / totals tables and the `entityKindScroll` slide-window helpers are removed.
* **tui navigation refactor (T1):** the per-project TUI now has a hierarchical navigation with three top-level zones — `01 // TASKS` (board / table / graph), `02 // STATS` (general / logs), `03 // SETTINGS` (config) — rendered as a two-line header (top kicker + sub-menu strip; the sub strip is suppressed when the active top has only one sub). Bindings: `tab` / `shift+tab` cycles top zones (sub always lands on the zone's first sub); `1` / `2` / `3` jump straight to a top; `,` / `/` cycles sub-menus inside the current top (no-op when only one sub); `0` / `ctrl+h` returns to the multi-project Home. `n` / `e` / `c` (new task / edit task / add comment) only fire on the Tasks zone. Help groups renamed to `Tasks · board lens` / `Tasks · table lens` / `Tasks · graph lens` / `Stats · general` / `Stats · logs` / `Settings · config`. T1 is purely structural — content placement and `left`/`right` semantics inside each lens are unchanged; reorganization and standardization arrive in T2 / T3.
* **tui home:** running `okt tui` outside a registered project (no `--project` / `--project-id`, CWD does not match any `root_path`) now opens a multi-project Home Screen instead of erroring. The Home renders every project as a card in the same visual language as a board column, with tags from `project_tags` displayed as filled-pill badges. Navigation: `↑/↓` select, `enter` opens the project's Board. The new global `ctrl+h` binding returns to the Home from any per-project view; the per-view tab bar is suppressed while on Home (Home is outside the `tab` rotation).
* **tui cd-on-exit:** the TUI now writes the absolute root path of the most recently opened project to a small handshake file (`$OKT_CD_FILE` → `$XDG_RUNTIME_DIR/okt-cd` → `$TMPDIR/okt-cd-$UID`) when it exits. `install.sh` and `install.ps1` install an `okt()` shell-wrapper function (bash, zsh, PowerShell) delimited by sentinels (`# >>> okt wrapper >>>` / `# <<< okt wrapper <<<`) that reads the file and `cd`s the parent shell into the project. Running the bare binary works exactly as before — only the post-exit `cd` is silently absent. New `uninstall.sh` / `uninstall.ps1` companions remove the sentinel-wrapped block surgically and leave unrelated rc-file content intact.
* **metrics summary:** new MCP tool `metrics.summary` aggregates per-AI-model behaviour over a period (`7d` / `30d` / `all`): errors recorded, errors searched, solutions added, like rate, and search-before-record ratio. Backed by a unified domain-event timeline (`error.recorded`, `error.searched`, `solution.added`, `solution.liked`, `solution.failed`, `solution.viewed_top`) emitted from `app.ErrorService` and persisted on the `events` table. Supports an optional `project_id` filter; defaults to the cross-project benchmark.
* **tui stats view:** new sixth top-level view (`6` / `STATS`) renders the same per-model benchmark inline. `← →` cycles the period (`7d` / `30d` / `all`), `r` refreshes. Wired through `app.MetricsService` so the TUI shares the exact aggregation logic with the MCP endpoint.
* **agent attribution:** `events`, `errors`, and `solutions` rows now denormalize the calling agent's `source` / `entrypoint` / `agent_model` / `agent_session_id`, carried via the `internal/activity` context. The MCP adapter coerces `_agent_model` on every tool call (calls without it return `validation_error` with self-describing guidance); the CLI reads `OMAKITEN_AGENT_MODEL` (and optional `OMAKITEN_AGENT_SESSION_ID`); the TUI reports `human`. System-internal callers (e.g. MCP `ReadResource`) bypass the validation and write empty attribution so they don't pollute the per-model metrics.
* **templates.show shadow validation:** when an active project resolves and the requested slug refers to a global template that the project shadows with an override of the same default kind (e.g. `pull-request` shadowed by `pr-concise` in this repo), the call hard-rejects with `validation_error`. The error `details` carry `requested_slug`, `active_slug`, and `project` so the agent can re-call the correct slug without a clarification round-trip — same self-describing-rejection pattern as `_agent_model` coercion. Calls outside any registered project (no resolution) preserve the legacy slug-only lookup so `okt mcp tools` discovery and CLI debug calls keep working. An explicit `project` / `project_id` that does not resolve propagates `project_not_found` instead of falling back. The `templates.show` MCP schema now declares the standard `project_id` / `project` / `cwd` selector properties.

### TUI internals

* **components:** introduce `internal/tui/components/{viewport,picker,detailscreen}` sub-packages — Bubble Tea sub-models that own cursor + scroll state for the surfaces previously tracked as flat fields on the root `Model`. No user-facing behaviour change; visual output, key bindings and screen contracts are preserved exactly. The sub-packages have standalone unit tests so the components can evolve independently of the screens that embed them.

### Architecture internals

* **agent/config:** split the protocol-neutral agent DTO/service surface and config loader internals by responsibility, preserving the existing hexagonal boundaries between adapters, application services, domain, and persistence.
* **hexagonal realignment:** workflow policy (default-bucket selection on create, transition allowed?, guards, task.completed-on-final emission) moves from `internal/sqlite` to a new `app.WorkflowService` that composes fine-grained ports (`WorkflowRepository`, `GuardEvaluationRepository`, plus existing `EventRepository`/`ConfigRepository`/`TaskRepository`). The sqlite Store keeps only pure-persistence variants of `CreateTask`/`MoveTask` plus the workflow primitives; integration tests now thread through the service.
* **configstore adapter:** `internal/app/{config_service,bundle_editor,law_service,persona_service,skill_service}.go` no longer call `internal/config` for I/O. A new `internal/configstore.Adapter` implements three new ports declared in `app/ports.go` — `BundleStore` (`LoadBundle`/`SaveBundle`/`HashFile`/`WriteAtomic`/`EnsureDefaultFiles`/`MigrateLayout`/`ConfigRootFromYAMLPath`), `EntityFileWriter` (`LawFileBytes`/`PersonaFileBytes`/`SkillFileBytes`/path helpers), and `Slugifier`. App services depend on the ports; the composition root injects the adapter once.
* **agent runtime → composition root:** `internal/agent/runtime.go` is gone; the bootstrap (path resolution, layout migration, default-file seeding, sqlite open, config import, template snapshots) now lives in a dedicated `internal/agentruntime` package. `internal/agent` carries only the `Service`, DTOs, and feature files — it no longer imports `internal/config`, `internal/paths`, or `internal/sqlite` from production code.
* **TUI orchestration → app:** the dependency-set diff (`saveBlockerPicker`), template default frontmatter rewrite, and the read fan-out for the refresh tick all moved into `app.DependencyService.SyncBlockers`, `app.TemplateService.SetDefault`, and `app.TUIQueryService.Snapshot`. The TUI's `refresh()` shrunk from ~80 lines to ~25; `internal/tui/entity.go` lost its `config` and `domain` imports as a result.
* **boundary enforcement:** new `internal/arch/arch_test.go` walks the import graph and fails if `internal/domain` reaches into adapters, if `internal/app` imports concrete adapters, or if any leaf adapter references a sibling. A `.golangci.yml` mirrors the rules under `depguard`. CI now runs `go vet`, `go test -race -count=1` and `golangci-lint`.
* **eliminated `"backlog"` literal in production code:** `internal/cli/add.go --bucket` defaults to `""` (the `WorkflowService` resolves the active workflow's first bucket); the TUI's create form does the same.

## [0.8.0](https://github.com/This-Is-NPC/omakiten/compare/v0.7.0...v0.8.0) (2026-05-07)


### Features

* bind laws/personas/skills/templates to MCP commands ([#25](https://github.com/This-Is-NPC/omakiten/issues/25)) ([13c4cd5](https://github.com/This-Is-NPC/omakiten/commit/13c4cd51db60f0dd34ab82486158b3c202004104))

## [0.7.0](https://github.com/This-Is-NPC/omakiten/compare/v0.6.0...v0.7.0) (2026-05-07)


### Features

* home screen for project selection with cd-on-exit ([#23](https://github.com/This-Is-NPC/omakiten/issues/23)) ([b6976b2](https://github.com/This-Is-NPC/omakiten/commit/b6976b2abfbab55de07dc5eaebbd449db2f8321b))

## [0.6.0](https://github.com/This-Is-NPC/omakiten/compare/v0.5.0...v0.6.0) (2026-05-06)


### Features

* **activity:** add task activity feed ([#21](https://github.com/This-Is-NPC/omakiten/issues/21)) ([acb0e49](https://github.com/This-Is-NPC/omakiten/commit/acb0e499744aff9a037a01e41efd8a53b966fc3a))
* **views:** sort and filter configuration across all views ([#19](https://github.com/This-Is-NPC/omakiten/issues/19)) ([40ad4fd](https://github.com/This-Is-NPC/omakiten/commit/40ad4fdaa2c94cf699ca44f0605d1cefb1de37f0))

## [0.5.0](https://github.com/This-Is-NPC/omakiten/compare/v0.4.0...v0.5.0) (2026-05-06)


### Features

* **config:** auto-load skills, laws and personas from their folders ([#17](https://github.com/This-Is-NPC/omakiten/issues/17)) ([77c7d0c](https://github.com/This-Is-NPC/omakiten/commit/77c7d0c74ca656bc149f5eee5cf5c3391cba09b8))
* **errors:** solution likes counter with cross-project top list ([#15](https://github.com/This-Is-NPC/omakiten/issues/15)) ([649ae41](https://github.com/This-Is-NPC/omakiten/commit/649ae41fb3c9632cff321ea290ac713e4574cfc2))
* **templates:** task templates, custom-overlay layout & project-scoped defaults ([#18](https://github.com/This-Is-NPC/omakiten/issues/18)) ([8b0953a](https://github.com/This-Is-NPC/omakiten/commit/8b0953a08afb6690c6270063e43257ceda8d2425))

## [0.4.0](https://github.com/This-Is-NPC/omakiten/compare/v0.3.0...v0.4.0) (2026-05-05)


### Features

* **errors:** Add error self-report registry with cross-project search ([#13](https://github.com/This-Is-NPC/omakiten/issues/13)) ([6cf1958](https://github.com/This-Is-NPC/omakiten/commit/6cf195877b0ad871875db9b6ada506cc1abdae4e))

## [0.3.0](https://github.com/This-Is-NPC/omakiten/compare/v0.2.0...v0.3.0) (2026-05-05)


### Features

* **guards:** transition guard evaluation and comment tagging ([#11](https://github.com/This-Is-NPC/omakiten/issues/11)) ([89cac57](https://github.com/This-Is-NPC/omakiten/commit/89cac570e44178ae8a2d8137e10abca68c938a5f))

## [0.2.0](https://github.com/This-Is-NPC/omakiten/compare/v0.1.1...v0.2.0) (2026-05-05)


### Features

* **graph:** add cursor navigation and task opening to dependency view ([#8](https://github.com/This-Is-NPC/omakiten/issues/8)) ([0e33b27](https://github.com/This-Is-NPC/omakiten/commit/0e33b27ef55deff5e9c974f032434c4a6c6f23bf))
* **tags:** Add reusable cross-project tags to tasks and projects ([#10](https://github.com/This-Is-NPC/omakiten/issues/10)) ([6a7ab73](https://github.com/This-Is-NPC/omakiten/commit/6a7ab7332bf6c990d33764c42cb7fda21c790674))

## [0.1.1](https://github.com/This-Is-NPC/omakiten/compare/v0.1.0...v0.1.1) (2026-05-05)


### Bug Fixes

* **tui:** add responsive scrolling and adaptive width to all views ([#6](https://github.com/This-Is-NPC/omakiten/issues/6)) ([0d55f4e](https://github.com/This-Is-NPC/omakiten/commit/0d55f4edfcb2cc97ebbe11a69a7ffbf8efadb595))

## 0.1.0 (2026-05-05)


### Features

* **activity:** add activity logging domain and infrastructure ([578b6fe](https://github.com/This-Is-NPC/omakiten/commit/578b6fe8d14940db88d17d9aa18df0dc85d74fbe))
* **activity:** add activity tracking to core services ([d8e41b3](https://github.com/This-Is-NPC/omakiten/commit/d8e41b38e8d81c0fda1e2991eb8d88884547efb4))
* add install scripts and update README with release one-liners ([d3e4572](https://github.com/This-Is-NPC/omakiten/commit/d3e4572f0095dd8e6decaca168c73c2539cddfac))
* **agent:** add protocol-neutral agent intent layer ([b95b21c](https://github.com/This-Is-NPC/omakiten/commit/b95b21c598259dc953af9bf9df88250b85433e14))
* **agentsetup:** add Claude Desktop MCP harness setup ([ef7188d](https://github.com/This-Is-NPC/omakiten/commit/ef7188d9e60682b05cba5229a9970a1f28ec241c))
* **app:** add law, persona, skill and editor services ([5a2033f](https://github.com/This-Is-NPC/omakiten/commit/5a2033f15212b519b0ad02b38013fc5b5bcaa188))
* **cli:** add law, persona, skill and editor commands ([42c8a4e](https://github.com/This-Is-NPC/omakiten/commit/42c8a4e31d1bbb6ec9132139bea210647b26c98b))
* **cli:** add MCP call, serve, and tools commands ([fe74595](https://github.com/This-Is-NPC/omakiten/commit/fe745952495620c9175d61e82605611da5c1c536))
* **cli:** add MCP commands and activity integration ([877ebb4](https://github.com/This-Is-NPC/omakiten/commit/877ebb4f82f41efcf9675ba5f8708c6fb8039dcb))
* **config:** add markdown entity loader, frontmatter parser and refactor bundle format ([4bb0be4](https://github.com/This-Is-NPC/omakiten/commit/4bb0be418468ddae05079a3e153bda851f1b42be))
* **defaults:** migrate bundled skills, laws and personas to markdown files ([191f7f8](https://github.com/This-Is-NPC/omakiten/commit/191f7f8ac5f59bb0c68078a4a9df05800f1b41b1))
* **domain:** introduce entity models, error codes and database migration ([8ae8cc3](https://github.com/This-Is-NPC/omakiten/commit/8ae8cc33e06682a4e0c2830917953f6a56403551))
* **mcp:** add MCP adapter and stdio server ([d4a93ed](https://github.com/This-Is-NPC/omakiten/commit/d4a93eded435fa1f3abc9212b04134d8d0d2d903))
* **mcp:** add OpenCode harness support and setup command ([b1d0f57](https://github.com/This-Is-NPC/omakiten/commit/b1d0f577f02ea01b6f1a532528d7415aa5ae11e5))
* **paths:** add OMAKITEN_HOME env override for portable config and data ([e3ad6e6](https://github.com/This-Is-NPC/omakiten/commit/e3ad6e64432d272e82377dbca437d60c462faa6a))
* **sqlite:** implement entity storage with migration and repository methods ([d5416c2](https://github.com/This-Is-NPC/omakiten/commit/d5416c2104ec1c07c1a86230229f9e79b9eff515))
* **task:** add priority support to task creation ([ad2f927](https://github.com/This-Is-NPC/omakiten/commit/ad2f927986dd1a7cb1bcf4d31ac2506fc3117948))
* **tui:** add activity log integration to TUI ([4e32187](https://github.com/This-Is-NPC/omakiten/commit/4e321873c7a16b3d7b0bbb981b64c5a3c3a35423))
* **tui:** add priority, blocker, and comment badges to task cards ([aba67eb](https://github.com/This-Is-NPC/omakiten/commit/aba67eb088142a01298751a333e727223e500b78))
* **tui:** add task priority editing and blocker picker ([813cbdb](https://github.com/This-Is-NPC/omakiten/commit/813cbdb2de80efb51e3c0e01fd148936f2cb9e13))
* **tui:** integrate entity browser, task forms and token metrics ([4092cea](https://github.com/This-Is-NPC/omakiten/commit/4092ceae65e64abf4648773c03dfa46a57e5225b))
* **tui:** render entities as cards and unify panels with shared-junction grid tables ([5b37078](https://github.com/This-Is-NPC/omakiten/commit/5b37078278dddf1338f70175d37065d43786e323))
* **tui:** responsive layout, delete confirmation, and contextual help ([863338d](https://github.com/This-Is-NPC/omakiten/commit/863338d6ca6bf62061edfe4310d29b47e6ecc222))


### Bug Fixes

* **gitignore:** anchor okt binary pattern so cmd/okt entrypoint is tracked ([6ed500f](https://github.com/This-Is-NPC/omakiten/commit/6ed500fc8536dde6c707ee44f2519892ddbb182d))
* **mcp:** remove bucket_key from create task schema ([623d1e2](https://github.com/This-Is-NPC/omakiten/commit/623d1e2bc969e36af9886a0cb3b9bb33d9db6012))
* **release:** force initial release to v0.1.0 ([#5](https://github.com/This-Is-NPC/omakiten/issues/5)) ([ec3dd29](https://github.com/This-Is-NPC/omakiten/commit/ec3dd29236763d1f055cfa6854b6ee715e8035f2))
* **release:** pin first version to 0.1.0 + add PR CI ([#4](https://github.com/This-Is-NPC/omakiten/issues/4)) ([dfe51d8](https://github.com/This-Is-NPC/omakiten/commit/dfe51d8904d0cb944e5ee50c0a25e6ec436f9a21))
* **sqlite:** default new tasks to first workflow bucket instead of hardcoded backlog ([8a1292c](https://github.com/This-Is-NPC/omakiten/commit/8a1292cc42240efe13cd928c33dae1eb48bc9636))
