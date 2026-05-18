# CLI Commands (`okt`)

`okt` is the CLI entrypoint for Omakiten (`cmd/okt/main.go` → `internal/cli/root.go:NewRootCommand`). Every command emits a single JSON envelope (`{"ok": true, "data": …}` on success, `{"ok": false, "error": {…}}` on failure) via `internal/output/json.go`. Failures carry the coded error from `internal/domain/errors.go`; the process exits with code `1`.

> All flag tables below are derived from source in `internal/cli/`. The shipped binary's `--help` may lag if it is older than the current source — when in doubt, the source is canonical.

## Global flags

Inherited by every subcommand (`internal/cli/root.go:NewRootCommand`):

| Flag | Type | Default | Effect |
|---|---|---|---|
| `--config` | string | resolved | Override path to the active profile yaml (any `.yaml` under `<config-dir>/` or `<config-dir>/custom/`). Highest precedence over `.active`, `$OMAKITEN_HOME`, and XDG. |
| `--db` | string | resolved | Override path to the SQLite database. |
| `--project`, `-p` | string | — | Active project slug. |
| `--project-id` | int | — | Active project id (numeric). |
| `--help`, `-h` | — | — | Help for the subcommand. |
| `--version`, `-v` | — | — | Print the binary version (root only). |

**Project resolution order** (`internal/project/resolver.go`): `--project-id` → `--project` → current working directory matching a registered project root.

**Path resolution** — full contract (precedence, `.active` lookup, `custom/` shadowing, discovery fallthrough, boot order) lives in [`reference/path-resolution.md`](./reference/path-resolution.md). TL;DR precedence: `--config` / `--db` flag → `$OMAKITEN_HOME` → `$XDG_CONFIG_HOME` / `$XDG_DATA_HOME` → `~/.config/omakiten` and `~/.local/share/omakiten`.

## Environment variables

| Variable | Effect |
|---|---|
| `OMAKITEN_HOME` | Overrides config + data root (see path resolution above). |
| `OMAKITEN_AGENT_MODEL` | Identifies the agent driving CLI calls; denormalized on every write (`events`, `errors`, `solutions`) and surfaced by `metrics.summary` / TUI Stats. Empty string is allowed and marks the call as non-benchmarked. |
| `OMAKITEN_AGENT_SESSION_ID` | Optional opaque session id; lets `metrics.summary`'s `search_before_record_ratio` correlate searches to records within a session. |
| `OKT_CD_FILE` · `XDG_RUNTIME_DIR` · `TMPDIR` | Resolution chain for the TUI `cd-on-exit` handshake file (see `okt tui` below). |
| `OKT_CLI_LANG` · `OKT_TUI_LANG` · `OKT_AGENT_LANG` | Skip the matching `okt setup` picker screen by pre-supplying the chosen value. `OKT_CLI_LANG` and `OKT_TUI_LANG` validate against bundled packs under `defaults/languages/`; `OKT_AGENT_LANG` is free-form. |
| `OKT_PRESET` · `OKT_HARNESSES` | Skip the preset and harness picker screens (`omakase` / CSV of harness names; `0` disables harness setup). |

The TUI sets `agent_model="human"` internally so its activity is filtered out of the per-model benchmark; it does not consult `OMAKITEN_AGENT_MODEL`.

---

## `okt setup` — post-install picker

`internal/cli/setup.go`. The bubbletea picker the curl-bash installer hands off to. Walks CLI language → TUI language → agent output language → workflow preset → MCP harnesses in one program, then writes the `okt()` shell-rc wrapper. Re-run with `--update` to revisit choices; existing rc-wrapper and `omakiten.yaml` settings are preserved.

The language pickers enumerate `defaults/languages/` at boot via `defaults.FS.ReadDir("languages")` — every bundled YAML auto-appears, with no allowlist to update. Adding a new pack ships as a doc-only PR on top of `defaults/languages/<code>.yaml`; see the [Languages Guide](./languages-guide.md) for the filename convention, header fields, parity rule, and the `scripts/new-language-pack.sh` scaffold.

Each picker screen is skipped when the matching env var or flag is set (`OKT_CLI_LANG` / `--cli-lang`, `OKT_TUI_LANG` / `--tui-lang`, `OKT_AGENT_LANG` / `--agent-lang`, `OKT_PRESET` / `--preset`, `OKT_HARNESSES` / `--harnesses`). Useful in CI, Dockerfiles, and dotfiles bootstrap.

```sh
okt setup                                    # interactive — needs a TTY
okt setup --update                           # re-prompt with current values prefilled
OKT_CLI_LANG=pt-br OKT_TUI_LANG=pt-br OKT_PRESET=omakase OKT_HARNESSES=0 \
  okt setup --skip-wrapper --skip-harnesses  # headless smoke (no rc-file write, no harness call)
```

---

## `okt init` — register the current project

`internal/cli/init.go`. Inserts a project row in the global SQLite DB (`UpsertProject`); optionally writes the MCP harness config.

| Flag | Default | Effect |
|---|---|---|
| `--name` | — | Project display name. |
| `--slug` | — | Project slug (kebab-case key). |
| `--root` | `$CWD` | Project root path used for CWD-based resolution. |
| `--enable-mcp` | `false` | Also configure an MCP harness entry. |
| `--mcp-harness` | `claude-code` | One of `claude-code`, `claude-desktop`, `opencode`, `crush`, `github-copilot`, `codex` (`internal/agentsetup/setup.go::SupportedHarnesses`). |
| `--mcp-config` | harness default | Override the harness config-file path. |
| `--mcp-command` | current executable | Command written into the harness config. |
| `--mcp-dry-run` | `false` | Preview changes without writing. |
| `--mcp-force` | `false` | Replace an existing `mcpServers.omakiten` entry. |
| `--preset` | — | Copy an official workflow preset from `defaults/config/<name>.yaml` into `<root>/.omakiten/config/<name>.yaml` and set `.active` to that basename. Without `--root`, a Git worktree is detected by walking up from `$CWD`; outside Git, `$CWD` is used. |
| `--preset-force` | `false` | Overwrite an existing `.omakiten` target when applying `--preset`. |

```sh
okt init --name Omakiten --slug omakiten --root "$PWD"
okt init --slug acme --enable-mcp --mcp-harness opencode --mcp-dry-run
okt init --preset kaiseki --name Acme --slug acme
```

Official presets are flat YAML starter files in `defaults/config/`: `omakase.yaml` (the canonical kit; full config + workflow), `izakaya.yaml`, `kaiseki.yaml`, and `shokunin.yaml`. Applying one writes only `.omakiten/config/<preset>.yaml` and points `.omakiten/config/.active` at it; the resolver finds it on the next invocation.

---

## `okt config presets` — list official workflow presets

`internal/cli/config.go`. Returns the bundled preset menu as JSON.

```sh
okt config presets
```

Presets:

| Preset | Style |
|---|---|
| `omakase` | Chef's choice: balanced backlog -> dev -> review -> done with self-branch, resume, and documentation guards. |
| `izakaya` | Casual: backlog -> dev -> done, no guards. |
| `kaiseki` | Multi-course: requirements -> planning -> dev -> review -> docs -> done with ritual guards. |
| `shokunin` | Artisan: kaiseki plus tests-passing and peer-review checkpoints. |

---

## Tasks

### `okt add` — create a task

`internal/cli/add.go`. Calls `app.TaskService.Add` → `app.WorkflowService.CreateTask`.

| Flag | Default | Effect |
|---|---|---|
| `--title`, `-t` | — | Task title. |
| `--description`, `-d` | — | Task description. |
| `--bucket`, `-b` | first active bucket | Bucket key. Empty falls back to `app.WorkflowService.ResolveDefaultBucket` — the **first bucket** of the active workflow, not a hard-coded `backlog`. |

```sh
okt add -t "Refactor sqlite store"
okt add -t "Doc cleanup" -d "Update guards.md" -b dev
```

### `okt list` — list tasks

`internal/cli/list.go`. Calls `app.TaskService.List`.

| Flag | Default | Effect |
|---|---|---|
| `--bucket`, `-b` | — | Filter by bucket key. Empty = all. |

```sh
okt list
okt list -b review
```

### `okt edit TASK_ID` — edit a task

`internal/cli/edit.go`. Only fields explicitly passed are updated (`cmd.Flags().Changed(...)`).

| Flag | Effect |
|---|---|
| `--title`, `-t` | Rewrite title. |
| `--description`, `-d` | Rewrite description. |
| `--priority` | Priority label or numeric id. Resolved against the active bundle via `parsePriority` (`internal/cli/enums.go`) → `*domain.EnumRegistry`. Out-of-the-box labels: `low`, `normal`, `high` from `config.priorities` in `defaults/config/omakase.yaml`; rename them by editing the YAML, no code change required. |
| `--bucket`, `-b` | Re-bucket through the workflow service (transition + guards still enforced). |

```sh
okt edit 42 --priority high
okt edit 42 -t "New title" -b done
```

### `okt move TASK_ID --to BUCKET` — move a task

`internal/cli/move.go`. Calls `app.TaskService.Move` → `app.WorkflowService.MoveTask` (transition allowance + guards + `task.completed` on final bucket; see `.docs/guards-guide.md`).

| Flag | Required | Effect |
|---|---|---|
| `--to`, `-t` | yes | Target bucket key. |

```sh
okt move 42 --to dev
```

### `okt delete TASK_ID --confirm` — hard-delete a task

`internal/cli/lifecycle.go`. Calls `app.TaskService.Delete`. Cascades through comments, event_tags, events, dependencies, tags, and the task row. Subject to bucket `permissions.task.delete` and `operations.delete.guards`. Emits `task.removed` with the pre-delete snapshot. Refuses to run without `--confirm` so a stray invocation cannot wipe data.

```sh
okt delete 42 --confirm
```

### `okt archive TASK_ID` — archive a task (escape hatch)

`internal/cli/lifecycle.go`. Calls `app.TaskService.Archive`. Flips `state=archived` and moves the task to the workflow's final bucket atomically. Bypasses bucket permissions and transition guards but respects `operations.archive.guards`. Emits `task.archived`. Archived tasks are hidden from `okt list` and the TUI views by default.

### `okt unarchive TASK_ID` — restore an archived task

`internal/cli/lifecycle.go`. Calls `app.TaskService.Unarchive`. Flips `state=active`, leaves the bucket untouched. Respects `operations.unarchive.guards` if declared. Emits `task.unarchived`.

```sh
okt archive 42
okt unarchive 42
```

### `okt assign TASK_ID [WHO]` — set or clear the task assignee

`internal/cli/assign.go`. Calls `app.TaskService.Assign`. Sets the free-text `tasks.assigned_to` column. Omitting `WHO` (or passing an empty string) clears the assignment back to NULL — the documented recovery path when a claiming agent crashes without finishing.

| Position | Required | Effect |
|---|---|---|
| `TASK_ID` | yes | Numeric task id. |
| `WHO` | no | Free-text claimant (`claude-opus-4-7`, `gabriel`, `tui`, …). Empty = clear. |

```sh
okt assign 42 claude-opus-4-7      # claim
okt assign 42                       # clear (recovery)
```

Emits `task.assigned` when set, `task.unassigned` when cleared. Listed at the top level (not under `okt task`) to match the existing flat surface (`okt move`, `okt edit`, `okt archive`, …).

---

## Comments

`internal/cli/comment.go`.

### `okt comment add TASK_ID`

| Flag | Default | Effect |
|---|---|---|
| `--body`, `-b` | — | **Required.** Comment body. |
| `--author`, `-a` | `human` | `human` or `agent`. |
| `--tag`, `-T` | — | Tag name (kebab-case-normalized). Repeatable: `-T resume -T deployment-notes`. |

### `okt comment list TASK_ID`

No flags. Lists comments for the task in chronological order.

### `okt comment edit COMMENT_ID`

Calls `app.CommentService.Edit`. Subject to bucket `permissions.comment.edit` (inherits from `permissions.task.edit` when no comment block is declared). Replaces the body and the tag set in one call. Emits `comment.edited`.

| Flag | Default | Effect |
|---|---|---|
| `--body`, `-b` | — | **Required.** New comment body. |
| `--tag`, `-T` | — | Tag name. Repeatable. The submitted set replaces the old tags entirely. |

### `okt comment delete COMMENT_ID --confirm`

Calls `app.CommentService.Remove`. Subject to bucket `permissions.comment.delete` (same inheritance rule). Emits `comment.removed` with the body snapshot. Refuses to run without `--confirm`.

```sh
okt comment add 42 -b "Branch: feature/foo" -T self-branch
okt comment list 42
okt comment edit 1234 -b "Branch: feature/foo (rebased)" -T self-branch -T rebased
okt comment delete 1234 --confirm
```

---

## Dependencies

`internal/cli/depend.go`. Project-scoped, cycle-prevented (`internal/graph/dependency.go:HasCycle`).

### `okt depend add TASK_ID --on DEPENDS_ON_TASK_ID`

| Flag | Required | Effect |
|---|---|---|
| `--on`, `-i` | yes | The task this task depends on. |

### `okt depend remove TASK_ID --on DEPENDS_ON_TASK_ID`

| Flag | Required | Effect |
|---|---|---|
| `--on`, `-i` | yes | Dependency to remove. |

### `okt depend list TASK_ID`

No flags. Returns dependencies for the task.

```sh
okt depend add 42 --on 41
okt depend list 42
okt depend remove 42 --on 41
```

---

## Plans

`internal/cli/plan.go`. WBS-style orchestration: a plan groups child tasks into ordered waves and feeds the multi-agent claim flow exposed via MCP. Project-scoped — every call resolves against the active project (`--project` / `--project-id` / CWD).

### `okt plan create SLUG --name NAME [--goal-body BODY]`

Creates a plan in the active project. `slug` is required (kebab-case, unique per project); `--name` is required; `--goal-body` (`-g`) accepts a markdown string for the goal + acceptance criteria. Emits `plan.created`.

### `okt plan list`

Lists every plan in the active project with rollups (status, done/total tasks, percentage, active wave).

### `okt plan show SLUG`

Returns the full plan: waves, tasks per wave, percentage, active wave, claimable tasks. Read-only.

### `okt plan wave-add SLUG NAME [--position N]`

Appends a wave to the plan. `--position` defaults to `0` (auto-assign the next slot); pass a positive integer to insert at that position. Emits `plan.wave_added`.

### `okt plan assign SLUG WAVE_ID TASK_ID`

Attaches an existing task to `(plan, wave)`. Re-assigning within the same plan is idempotent.

### `okt plan claim SLUG`

Atomically reserves the next unblocked task in the plan's active wave. The CLI form is the human-driven counterpart to the MCP `plans.claim_next` tool: it inherits the same `BEGIN IMMEDIATE` serialisation and stamps `tasks.assigned_to` with the resolved agent identity (`OMAKITEN_AGENT_MODEL`, falling back to empty when unset). Returns `{claimed: false}` when nothing is currently claimable.

```sh
okt plan create plans-v1 --name "Plans rollout" --goal-body "Land WBS in 3 waves"
okt plan wave-add plans-v1 "Schema"
okt plan wave-add plans-v1 "MCP surface"
okt plan assign plans-v1 1 42
okt plan show plans-v1
OMAKITEN_AGENT_MODEL=claude-opus-4-7 okt plan claim plans-v1
```

To clear an abandoned claim, run `okt assign <task_id>` (no `WHO`) or move the task back to `backlog`.

---

## Context (handoff state)

`internal/cli/context.go`. Backed by `app.ContextService`.

### `okt context add`

| Flag | Required | Effect |
|---|---|---|
| `--body`, `-b` | yes | Context body (free-form markdown). Token-estimated on insert. |

### `okt context dump`

Dumps progressive context under the YAML token budget (`config.context.max_tokens`).

| Flag | Default | Effect |
|---|---|---|
| `--level`, `-l` | `2` | `1` = context entries only · `2` adds workflow + tasks + dependencies · `3` adds comments + active laws (`internal/app/context_service.go`). |

```sh
okt context add -b "Resuming from PR #17"
okt context dump -l 3
```

---

## Workflow

`internal/cli/workflow.go`.

### `okt workflow show`

No flags. Prints the active workflow's buckets and transitions (`config.workflow.active`).

### `okt workflow orphans` — rebind tasks orphaned by a workflow swap

Tasks that pointed to a bucket the previous active workflow had but the new
one does not are *orphans*: they still live in the database but no longer
belong to any active bucket. This command previews them and, on `--confirm`,
rebinds each one to the matching key in the new workflow (when preserved) or
to the first active bucket (when the key was removed).

```sh
okt workflow orphans            # preview only — exits validation_error with the migration plan
okt workflow orphans --confirm  # apply the rebind; emits task.migrated per task
okt workflow orphans --dry-run  # equivalent to running without --confirm
```

The preview envelope details carry the per-group plan (`from_bucket_key →
to_bucket_key (count)`) plus every affected task id. Empty preview reports
`applied: false, total: 0` with `ok: true`. Used inside the TUI by the
orphan-migration notification's `Migrate` button.

---

## Config

`internal/cli/config.go`.

### `okt config validate [path]`

Runs `config.ValidateBundle` against the supplied YAML (or the resolved one). Reports the first failing rule from `internal/config/validator.go`.

```sh
okt config validate
okt config validate ./omakase.yaml
```

### `okt config init --scope <global|local> --preset <name> [--force]`

`internal/cli/config_init.go`. Materialises a **standalone** install of the chosen preset:

| Scope | Destination |
|---|---|
| `global` | `paths.ConfigRoot()` (or the parent of `--config`'s yaml). |
| `local`  | `<cwd>/.omakiten/` (literal CWD; no walk-up). |

Both scopes share `config.SeedInstall` internally, which copies every embedded shipped file (skills, laws, personas, templates, themes, notifications, every preset yaml) and sets `.active` to the chosen preset. `--force` re-copies the shipped files (preserving every `custom/` subtree).

Rerun matrix:
- Same preset, same files → `no_op:true`.
- Different preset → flips `.active`, no `no_op`.
- Tampered shipped file, no force → preserved.
- Tampered shipped file, `--force` → restored, `refreshed:true`.

### `okt config show --scope <global|local>`

`internal/cli/config_show.go`. Prints the raw bytes of the chosen scope's active yaml. Local walks up from CWD via `config.FindRepoLocal`; missing discovery is a `validation_error` (no silent fallback to global — the standalone semantics make a fallback ambiguous).

### `okt config path --scope <global|local>`

`internal/cli/config_show.go`. Prints the install root directory the layer owns (the `ConfigRoot` for global, the discovered `.omakiten/` for local).

### `okt config why <key> [--layer <global|local>]`

`internal/cli/config_inspect.go`. Walks the active config by dotted YAML key path and reports `{key, value, source, path}`. Without `--layer` the runtime resolver decides — the discovered `.omakiten/` wins over the user-global ConfigRoot. With `--layer` the lookup is pinned to that scope. Missing keys (and missing local installs when `--layer local`) return `source = "not_set"`.

### `okt config diff <left> <right>`

`internal/cli/config_inspect.go`. Structural YAML diff between two sources. Each operand is one of:

- `global` — the user-global active yaml.
- `local` — the active yaml inside the CWD walk-up `.omakiten/`.
- `local:<path>` — the active yaml inside `<path>/.omakiten/`.
- any other string → treated as a literal yaml file path.

Output entries carry `op = added | removed | changed` plus the relevant side values. Maps descend recursively; lists and scalars compare by deep equality.

---

## Skills

File-backed under `skills/<slug>.md`. `internal/cli/skill.go`. Frontmatter: `name`, `description?` (`internal/config/entity_loader.go:skillFrontmatter`).

> `SLUG` arguments accept either the slug **or** the numeric SQLite id (back-compat fallback in `internal/cli/editor.go:resolveSkillSlug`).

### `okt skill list`
No flags.

### `okt skill show SLUG`
No flags.

### `okt skill add`

| Flag | Required | Effect |
|---|---|---|
| `--name`, `-n` | yes | Display name. |
| `--key`, `-k` | derived | Slug. Defaults to `Slugify(--name)` when omitted. |
| `--description`, `-d` | — | Short description. |
| `--no-edit` | `false` | Skip opening `$EDITOR` on the new file. |

After creating the scaffold, the command opens `$EDITOR` (then `$VISUAL`, then `nano`; resolved by `app.ResolveEditor`) and re-imports the bundle on save, unless `--no-edit` is set.

### `okt skill edit SLUG`

| Flag | Effect |
|---|---|
| `--name`, `-n` | Rewrite display name. |
| `--description`, `-d` | Rewrite description. |
| `--no-edit` | Apply only flag-driven updates; skip `$EDITOR`. |

### `okt skill remove SLUG`
No flags. Deletes the file and prunes references in personas (`internal/app/skill_service.go`).

```sh
okt skill add -n "Go" -d "Idiomatic backend Go"
okt skill edit go --description "Idiomatic Go for CLIs and services"
okt skill remove go
```

---

## Laws

File-backed under `laws/<slug>.md`. `internal/cli/law.go`. Frontmatter: `name?`, `severity` (`info`/`warning`/`error`).

> `SLUG` arguments accept either the slug or the numeric id.

### `okt law list`

| Flag | Effect |
|---|---|
| `--scope` | Filter by `global`, `project`, or `persona`. |
| `--project` | Filter by project slug. |
| `--persona` | Filter by persona slug. |

### `okt law show SLUG`
No flags.

### `okt law add`

| Flag | Required | Default | Effect |
|---|---|---|---|
| `--key`, `-k` | yes | — | Law slug (kebab-case). |
| `--name`, `-n` | — | — | Display name (optional). |
| `--severity`, `-s` | — | `error` | `info`, `warning`, or `error`. |
| `--body`, `-b` | — | placeholder | Body. Empty triggers the placeholder + `$EDITOR`. |
| `--scope` | — | `global` | `global`, `project`, or `persona`. |
| `--project` | when `--scope=project` | — | Project slug owning the law. |
| `--persona` | when `--scope=persona` | — | Persona slug owning the law. |
| `--no-edit` | — | `false` | Skip opening `$EDITOR`. |

### `okt law edit SLUG`

| Flag | Effect |
|---|---|
| `--name`, `-n` | Rewrite display name. |
| `--severity`, `-s` | Rewrite severity. |
| `--body`, `-b` | Rewrite body. |
| `--no-edit` | Apply only flag-driven updates; skip `$EDITOR`. |

### `okt law remove SLUG`
No flags. Deletes the file and prunes references.

```sh
okt law add -k workflow-enforced -n "Workflow Enforced" -s error
okt law list --scope persona --persona engineer
```

---

## Personas

File-backed under `personas/<slug>.md`. `internal/cli/persona.go`. Wiring (skill refs) lives in the active profile yaml's `personas:` section.

> `SLUG` arguments accept either the slug or the numeric id.

### `okt persona list`
No flags.

### `okt persona show SLUG`
No flags. Includes frontmatter, body, and resolved skill refs.

### `okt persona add`

| Flag | Required | Effect |
|---|---|---|
| `--name`, `-n` | yes | Display name. |
| `--key`, `-k` | — | Slug. Defaults to `Slugify(--name)`. |
| `--description`, `-d` | — | Short description. |
| `--skill`, `-s` | — | Skill **id** (repeatable: `-s 1 -s 2`). |
| `--skill-slug` | — | Skill **slug** (repeatable). |
| `--no-edit` | — | Skip opening `$EDITOR`. |

### `okt persona edit SLUG`

| Flag | Effect |
|---|---|
| `--name`, `-n` | Rewrite display name. |
| `--description`, `-d` | Rewrite description. |
| `--skill`, `-s` | **Replaces** the skill id set. |
| `--skill-slug` | **Replaces** the skill slug set. |
| `--no-edit` | Apply only flag-driven updates; skip `$EDITOR`. |

### `okt persona remove SLUG`
No flags.

```sh
okt persona add -n "Engineer" --skill-slug go --skill-slug sqlite
okt persona edit engineer --skill-slug go --skill-slug cli   # replaces full skill set
```

---

## TUI

### `okt tui` — open the terminal UI

`internal/cli/tui.go`. Loads the active theme (`<root>/themes/<active>.yaml`, with `themes/custom/<active>.yaml` overriding) and starts Bubble Tea in alt-screen mode. Per-project navigation is hierarchical (`internal/tui/state.go`): three top zones — Tasks (board / table / graph), Stats (general / logs), Settings (general / laws / personas / skills / templates / tags) — plus a multi-project Home sentinel reachable via `0` or `ctrl+h`. The CLI plumbs the resolved active-profile path, SQLite path, and `okt` version into the TUI so Settings › General can surface them.

**Project-resolution behavior on launch:**

- With `--project` / `--project-id`, or when `$CWD` matches a registered project root, the TUI opens directly on that project's Board (existing behavior).
- Without any of the above, the TUI opens the Home Screen listing every registered project. Selecting one loads its Board normally. See [TUI Guide → Home](tui-guide.md#home-multi-project-picker).

No flags beyond globals.

```sh
okt --project omakiten tui
okt tui                           # outside a registered project: opens Home
```

**`cd-on-exit` shell wrapper:**

`install.sh` / `install.ps1` install an `okt()` shell function (bash, zsh, PowerShell) that wraps the binary. When the TUI exits with a project loaded, the wrapper `cd`s the parent shell into that project's `root_path` — closing the TUI feels like a `cd` into the project you just worked on.

The wrapper is delimited by sentinel comments (`# >>> okt wrapper >>>` / `# <<< okt wrapper <<<`) in your `~/.bashrc` / `~/.zshrc` / `$PROFILE`, and is fully removed by `uninstall.sh` / `uninstall.ps1`. Running `okt` without the wrapper is supported — the TUI works normally; only the post-exit `cd` is silently absent.

The handshake file the wrapper reads can be overridden via `$OKT_CD_FILE`; defaults to `$XDG_RUNTIME_DIR/okt-cd` (or `$TMPDIR/okt-cd-$UID` as a last fallback).

---

## MCP

`internal/cli/mcp.go`. Adapter and stdio server in `internal/mcp/`.

### `okt mcp tools`

Lists tool/resource/prompt definitions (`mcp.Tools()`, `mcp.Resources()`, `mcp.Prompts()`). No flags.

### `okt mcp call TOOL_NAME`

Calls a tool directly without going through the stdio server. Useful for scripting and testing.

| Flag | Effect |
|---|---|
| `--input` | JSON object passed as the tool input (e.g. `'{"task_id":42}'`). |

### `okt mcp serve`

Runs the JSON-RPC 2.0 stdio server (`internal/mcp/server.go:Serve`). No flags. Stdin/stdout are the transport — meant to be spawned by an MCP harness.

### `okt mcp setup`

Writes the `omakiten` MCP server entry into a harness config file (`internal/agentsetup/setup.go`). Mirrors the `--mcp-*` flags exposed by `okt init` but as standalone subcommand flags:

| Flag | Default | Effect |
|---|---|---|
| `--harness` | `claude-code` | One of `claude-code`, `claude-desktop`, `opencode`, `crush`, `github-copilot`, `codex` (`internal/agentsetup/setup.go::SupportedHarnesses`). |
| `--config-path` | harness default | Override the harness config-file path. |
| `--command` | current executable | Command written into the harness config. |
| `--dry-run` | `false` | Preview changes without writing. |
| `--force` | `false` | Replace an existing entry. |

```sh
okt mcp tools
okt mcp call tasks.list --input '{"bucket_key":"dev"}'
okt mcp call search --input '{"query":"sqlite race","entity_types":["error","solution"]}'
okt mcp setup --harness opencode --dry-run
okt mcp serve   # invoked by the harness, not by hand
```

`okt mcp call search` is the CLI handle for the unified FTS5 surface (`internal/app/search_service.go`); it returns BM25-ranked hits with `<mark>...</mark>` snippets across tasks, comments, errors, solutions, and context entries. Pass `entity_types: []` (or omit the key) for an all-five sweep — the legacy `errors.search` MCP tool was retired alongside it.

The full set of MCP tools, resources, and prompts is documented in `.docs/mcp-guide.md`.

---

## Output envelope

Every command writes to stdout one of:

```json
{"ok":true,"data": …}
```

```json
{"ok":false,"error":{"code":"<coded>","msg":"…","details":{…}}}
```

`code` is one of the constants in `internal/domain/errors.go` (e.g., `validation_error`, `task_not_found`, `workflow_invalid_transition`, `guard_violation`, `dependency_invalid`, `tag_conflict`, `editor_failed`, `config_invalid`). The full list and agent-side guidance is in `.docs/mcp-guide.md` §"Failure Guidance".

A failed command exits with status `1`. JSON minification follows `config.output.json_minified`.
