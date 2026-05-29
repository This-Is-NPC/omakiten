# CLI Commands (`okt`)

`okt` is the CLI entrypoint for Omakiten (`cmd/okt/main.go` → `internal/cli/root.go:NewRootCommand`). Every command emits a single JSON envelope (`{"ok": true, "data": …}` on success, `{"ok": false, "error": {…}}` on failure) via `internal/output/json.go`. Failures carry the coded error from `internal/domain/errors.go`; the process exits with code `1`.

> All flag tables below are derived from source in `internal/cli/`. The shipped binary's `--help` may lag if it is older than the current source — when in doubt, the source is canonical.

## Contents

- [Global flags](#global-flags)
- [Environment variables](#environment-variables)
- [`okt setup` — post-install picker](#okt-setup--post-install-picker)
- [`okt update` — fetch latest release and swap the binary](#okt-update--fetch-latest-release-and-swap-the-binary)
- [`okt uninstall` — remove the binary and shell-rc wrapper](#okt-uninstall--remove-the-binary-and-shell-rc-wrapper)
- [`okt init` — register the current project](#okt-init--register-the-current-project)
- [`okt config presets` — list official workflow presets](#okt-config-presets--list-official-workflow-presets)
- [Tasks](#tasks)
- [Comments](#comments)
- [Dependencies](#dependencies)
- [Plans](#plans)
- [Context (handoff state)](#context-handoff-state)
- [Logs (event inspector)](#logs-event-inspector)
- [Workflow](#workflow)
- [Config](#config)
- [Database](#database)
- [Skills](#skills)
- [Laws](#laws)
- [Personas](#personas)
- [TUI](#tui)
- [MCP](#mcp)
- [Output envelope](#output-envelope)
- [See also](#see-also)

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

**Path resolution** — full contract (precedence, `.active` lookup, `custom/` shadowing, discovery fallthrough, boot order) lives in [`configuration-guide/path-resolution.md`](./configuration-guide/path-resolution.md). TL;DR precedence: `--config` / `--db` flag → `$OMAKITEN_HOME` → `$XDG_CONFIG_HOME` / `$XDG_DATA_HOME` → `~/.config/omakiten` and `~/.local/share/omakiten`.

## Environment variables

| Variable | Effect |
|---|---|
| `OMAKITEN_HOME` | Overrides config + data root (see path resolution above). |
| `OMAKITEN_AGENT_MODEL` | Identifies the agent driving CLI calls; denormalized on every write (`events`, `errors`, `solutions`) and surfaced by `metrics.summary` / TUI Stats. Empty string is allowed for most CLI calls and marks the call as non-benchmarked; `okt plan claim` is the exception and rejects an empty value because ownership claims must name an agent. |
| `OMAKITEN_AGENT_SESSION_ID` | Optional opaque session id; lets `metrics.summary`'s `search_before_record_ratio` correlate searches to records within a session. |
| `OKT_CD_FILE` · `XDG_RUNTIME_DIR` · `TMPDIR` | Resolution chain for the TUI `cd-on-exit` handshake file (see `okt tui` below). |
| `OKT_CLI_LANG` · `OKT_TUI_LANG` · `OKT_AGENT_LANG` | Pre-supply `okt setup` language inputs. CLI/TUI share one picker screen: either `OKT_CLI_LANG` or `OKT_TUI_LANG` resolves both fields (`OKT_CLI_LANG` wins if both are set). `OKT_AGENT_LANG` fills the separate agent-output language input. |
| `OKT_PRESET` · `OKT_HARNESSES` | Skip the preset and harness picker screens (`omakase` / CSV of harness names; `0` disables harness setup). |

The TUI sets `agent_model="human"` internally so its activity is filtered out of the per-model benchmark; it does not consult `OMAKITEN_AGENT_MODEL`.

---

## `okt setup` — post-install picker

`internal/cli/setup.go`. The bubbletea picker the curl-bash installer hands off to. Walks language → agent output language → workflow preset → MCP harnesses in one program, then writes the `okt()` shell-rc wrapper. CLI and TUI share the install-time language picker; the per-surface split lives in the profile yaml and can be changed later with `okt config language`. Re-run with `--update` to revisit choices; existing rc-wrapper and `omakiten.yaml` settings are preserved.

The language pickers enumerate `defaults/languages/` at boot via `defaults.FS.ReadDir("languages")` — every bundled YAML auto-appears, with no allowlist to update. Adding a new pack ships as a doc-only PR on top of `defaults/languages/<code>.yaml`; see the [Languages Guide](./configuration-guide/languages.md) for the filename convention, header fields, parity rule, and the `scripts/new-language-pack.sh` scaffold.

Each picker screen is skipped when the matching env var or flag is set (`OKT_CLI_LANG` / `--cli-lang`, `OKT_TUI_LANG` / `--tui-lang`, `OKT_AGENT_LANG` / `--agent-lang`, `OKT_PRESET` / `--preset`, `OKT_HARNESSES` / `--harnesses`). For the shared CLI/TUI language screen, either `OKT_CLI_LANG` or `OKT_TUI_LANG` resolves both fields; `OKT_CLI_LANG` wins when both are set, and the other field mirrors it unless supplied explicitly. Useful in CI, Dockerfiles, and dotfiles bootstrap.

```sh
okt setup                                    # interactive — needs a TTY
okt setup --update                           # re-prompt with current values prefilled
OKT_CLI_LANG=pt-br OKT_TUI_LANG=pt-br OKT_PRESET=omakase OKT_HARNESSES=0 \
  okt setup --skip-wrapper --skip-harnesses  # headless smoke (no rc-file write, no harness call)
```

---

## `okt update` — fetch latest release and swap the binary

`internal/cli/update.go`. The in-binary counterpart of the curl|bash refresh path. Resolves the running binary via `os.Executable`, queries `https://api.github.com/repos/This-Is-NPC/omakiten/releases/latest`, downloads the matching asset (`okt_<OS>_<arch>.tar.gz` on POSIX, `.zip` on Windows), verifies its SHA256 against `checksums.txt` from the same release, extracts the `okt` entry from the archive, and atomically replaces the binary with a sibling temp file + rename.

Flags: `--check` is a dry-run that prints `current=<v> latest=<v> action=<noop|upgrade>` and exits; `--yes` / `-y` skips the confirmation prompt for non-interactive callers.

JSON envelope codes (under `data.code`):
- `update_available` — `--check` saw a newer tag; nothing written.
- `update_not_required` — current matches latest (both `--check` and apply paths).
- `update_completed` — swap applied successfully.
- `update_failed` — any failure across fetch / checksum / download / extract / swap.
- `validation_error` — dev build (no version baked in), no TTY without `--yes`, or user declined the confirmation prompt.

```sh
okt update --check                  # report only — exits 0 on both upgrade-available and noop
okt update --yes                    # apply non-interactively
okt update                          # interactive y/n confirmation (needs a TTY)
```

Windows holds the EXE handle for any process running the binary, so the atomic swap can't replace it in place during a self-update. The current cut targets POSIX user-local installs (`$HOME/.local/bin/okt`); Windows callers should grab the new tarball manually until the `.exe.old` swap-on-exit lands in a follow-up.

---

## `okt uninstall` — remove the binary and shell-rc wrapper

`internal/cli/uninstall.go`. The in-binary counterpart of `uninstall.sh` / `uninstall.ps1`. Removes the binary at `$INSTALL_DIR` (defaults to `~/.local/bin` on POSIX, `%LOCALAPPDATA%\Programs\okt` on Windows) and strips the `okt()` wrapper block from every rc/profile target the installer wrote into (`.bashrc`, `.zshrc`, `Documents\PowerShell\profile.ps1`, `Documents\WindowsPowerShell\profile.ps1`).

Local state — the SQLite DB under `$XDG_DATA_HOME/omakiten` and the active yaml profile + entity folders under `$XDG_CONFIG_HOME/omakiten` — is preserved by default. Purge flags opt back into deletion; the picker shows each target's on-disk size and a `THIS CANNOT BE UNDONE` line before toggling either checkbox.

Flags: `--yes` / `-y` skips the picker for non-interactive callers; `--purge-data` also deletes `$XDG_DATA_HOME/omakiten` (SQLite DB + WAL/SHM); `--purge-config` also deletes `$XDG_CONFIG_HOME/omakiten` (active yaml + entity folders); `--purge` is shorthand for both. All purges are irreversible.

JSON envelope codes (under `data.code`): `uninstall_completed`, `uninstall_failed`, `validation_error`.

```sh
okt uninstall                       # interactive picker (needs a TTY)
okt uninstall --yes                 # remove binary + wrappers, keep data + config
okt uninstall --yes --purge         # remove everything — DB, config, binary, wrappers
okt uninstall --yes --purge-data    # keep config, drop the SQLite database
```

The bundled `uninstall.sh` / `uninstall.ps1` scripts stay in the repo for the bootstrap-failure case (the user never finished `okt setup`, so there's no binary to invoke). Both surfaces share `internal/lifecycle/*` primitives, so the rc-file scrub matches byte-for-byte across them.

---

## `okt init` — register the current project

`internal/cli/init.go`. Inserts a project row in the global SQLite DB (`UpsertProject`); optionally writes the MCP harness config and seeds a preset under `.omakiten/`.

Run `okt init --help` for the full flag list. Non-obvious behavior:

- `--root` defaults to `$CWD`; the value is what later CWD-based project resolution matches against.
- `--enable-mcp` toggles a bundle of `--mcp-*` flags that mirror [`okt mcp setup`](#okt-mcp-setup) — see that section for the harness matrix.
- `--preset NAME` copies `defaults/config/<NAME>.yaml` into `<root>/.omakiten/config/` and sets `.active`. Without `--root`, a Git worktree is detected by walking up from `$CWD`; outside Git, `$CWD` is used. Use `--preset-force` to overwrite an existing `.omakiten` target.

```sh
okt init --name Omakiten --slug omakiten --root "$PWD"
okt init --slug acme --enable-mcp --mcp-harness opencode --mcp-dry-run
okt init --preset kaiseki --name Acme --slug acme
```

Official presets are flat YAML starter files in `defaults/config/`: `omakase.yaml` (the canonical kit; full config + workflow), `izakaya.yaml`, `kaiseki.yaml`, and `shokunin.yaml`. Applying one invokes `config.SeedInstall`, which materialises the full install under `.omakiten/`: the preset YAML, `.active`, and every default entity (skills, laws, personas, templates, themes, notifications) for that preset; the resolver finds it on the next invocation.

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

Run `okt <subcommand> --help` for the full flag list of any task command. Notes below cover behavior not obvious from `--help`.

### `okt add` — create a task

`internal/cli/add.go`. Calls `app.TaskService.Add` → `app.WorkflowService.CreateTask`. When `--parent` is set, routes through `app.TaskService.AddSub` so the row + FK land in a single atomic INSERT.

- `--bucket` empty falls back to `app.WorkflowService.ResolveDefaultBucket` — the **first bucket** of the active workflow, not a hard-coded `backlog`.
- `--parent` requires an active task in the same project; cross-bucket parents are rejected; sub-task inherits the parent's bucket when `--bucket` is omitted.

```sh
okt add -t "Refactor sqlite store"
okt add -t "Doc cleanup" -d "Update guards.md" -b dev
okt add -t "Extract helper" --parent 42
```

### `okt list` — list tasks

`internal/cli/list.go`. Calls `app.TaskService.List`. `--parent` is tri-state via `cmd.Flags().Changed("parent")`: omit for no filter, pass `0` for roots only (`parent_id IS NULL`), pass a positive id for that parent's direct children.

```sh
okt list
okt list -b review
okt list --parent 0    # roots only
okt list --parent 42   # direct children of #42
```

### `okt edit TASK_ID` — edit a task

`internal/cli/edit.go`. Only fields explicitly passed are updated (`cmd.Flags().Changed(...)`).

- `--priority` accepts a label or numeric id resolved against the active bundle via `parsePriority` (`internal/cli/enums.go`). Out-of-the-box labels (`low`, `normal`, `high`) come from `config.priorities` in `defaults/config/omakase.yaml`; rename them by editing the YAML.
- `--bucket` re-buckets through the workflow service (transition guards still enforced; see [`configuration-guide/guards.md`](./configuration-guide/guards.md)).
- `--parent` is tri-state: omit to leave `parent_id` untouched, pass `0` to clear (becomes a root), pass a positive id to re-parent with anti-cycle enforcement.

```sh
okt edit 42 --priority high
okt edit 42 -t "New title" -b done
okt edit 99 --parent 42   # re-parent under #42
okt edit 99 --parent 0    # clear parent (becomes a root)
```

### `okt move TASK_ID --to BUCKET` — move a task

`internal/cli/move.go`. Calls `app.TaskService.Move` → `app.WorkflowService.MoveTask` (transition allowance + guards + `task.completed` on final bucket; see [`configuration-guide/guards.md`](./configuration-guide/guards.md)).

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

`internal/cli/assign.go`. Calls `app.TaskService.Assign`. Sets the free-text `tasks.assigned_to` column. Omitting `WHO` (or passing an empty string) clears the assignment back to NULL — the documented recovery path when a claiming agent crashes without finishing. Emits `task.assigned` when set, `task.unassigned` when cleared.

```sh
okt assign 42 claude-opus-4-7      # claim
okt assign 42                       # clear (recovery)
```

---

## Comments

`internal/cli/comment.go`. Run `okt comment <subcommand> --help` for the full flag list.

- `okt comment add TASK_ID -b BODY` — `--author` defaults to `human` (other value: `agent`); `--tag` / `-T` is repeatable and kebab-case-normalized (`-T resume -T deployment-notes`).
- `okt comment list TASK_ID` — chronological order, no flags.
- `okt comment edit COMMENT_ID -b BODY [-T …]` — replaces body and tag set in one call. The submitted `--tag` set **replaces** the old one entirely. Subject to bucket `permissions.comment.edit` (inherits from `permissions.task.edit` when no comment block is declared). Emits `comment.edited`.
- `okt comment delete COMMENT_ID --confirm` — subject to `permissions.comment.delete` (same inheritance rule). Emits `comment.removed` with the body snapshot. Refuses to run without `--confirm`.

```sh
okt comment add 42 -b "Branch: feature/foo" -T self-branch
okt comment list 42
okt comment edit 1234 -b "Branch: feature/foo (rebased)" -T self-branch -T rebased
okt comment delete 1234 --confirm
```

---

## Dependencies

`internal/cli/depend.go`. Project-scoped, cycle-prevented (`internal/graph/dependency.go:HasCycle`). Three subcommands share the same `--on` / `-i` flag (target dependency task id):

- `okt depend add TASK_ID --on DEP_ID`
- `okt depend remove TASK_ID --on DEP_ID`
- `okt depend list TASK_ID` (no flags)

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

Atomically reserves the next claimable task in the plan's active wave (unassigned and still in the workflow's first bucket) by stamping `tasks.assigned_to` with the resolved agent identity from `OMAKITEN_AGENT_MODEL`. The value is required; an empty or unset model returns `validation_error`. The CLI form is the human-driven counterpart to the MCP `plans.claim_next` tool and inherits the same `BEGIN IMMEDIATE` serialisation. **The bucket is not moved** — the claim is ownership-only, so the workflow's first bucket stays the task's bucket. Returns `{claimed: false}` when nothing is currently claimable.

```sh
okt plan create plans-v1 --name "Plans rollout" --goal-body "Land WBS in 3 waves"
okt plan wave-add plans-v1 "Schema"
okt plan wave-add plans-v1 "MCP surface"
okt plan assign plans-v1 1 42
okt plan show plans-v1
OMAKITEN_AGENT_MODEL=claude-opus-4-7 okt plan claim plans-v1
# Then move the claimed task through the workflow, honouring preset guards:
okt move 42 dev
```

The bucket transition is a separate `okt move` (or MCP `tasks.move`) call so preset-defined guards on the bucket transition stay authoritative — for example, omakase requires a self-branch comment before `backlog → dev`, and `claim` does not bypass it.

To clear an abandoned claim, run `okt assign <task_id>` (no `WHO`) or move the task back to `backlog`.

---

## Context (handoff state)

`internal/cli/context.go`. Backed by `app.ContextService`.

- `okt context add -b BODY` — `--body` required; body is free-form markdown, token-estimated on insert.
- `okt context dump [-l LEVEL]` — dumps progressive context under the YAML token budget (`config.context.max_tokens`). Levels: `1` = context entries only; `2` (default) adds workflow + tasks + dependencies; `3` adds comments + active laws (`internal/app/context_service.go`).

```sh
okt context add -b "Resuming from PR #17"
okt context dump -l 3
```

---

## Logs (event inspector)

`internal/cli/logs.go`. Reads the unified `events` table via `internal/sqlite.Store.ListEvents` — the same path the TUI Logs inspector consumes — and projects each row through `domain.SummarizeEvent` for the `summary` column.

Default scope is the last `views.logs.window_days` of every category for the resolved project (configured under `config.views.logs.window_days`, exposed as `Snapshot.LogsWindowDays()`).

| Flag | Type | Default | Effect |
|---|---|---|---|
| `--category`, `-c` | `[]string` | — | Restrict to one or more event categories. Repeatable (`-c task -c plan`) **and** comma-separated (`-c task,plan`). Token `all` clears the filter. Unknown tokens fail with `validation_error`. |
| `--since` | duration | settings window | Override the time floor. Accepts `time.ParseDuration` strings (`24h`, `30m`, …) plus an extra `Nd` shorthand for whole days (`7d`). |
| `--limit`, `-n` | int | `0` (no cap) | Cap the number of rows returned. |

Accepted categories (mirror the TUI filter chips): `task`, `comment`, `plan`, `tag-dep`, `guard`, `audit`, `hook`, `tool_call`, `trick`, `domain`.

Each emitted row carries the 5-field shape `time · event_type · entity · author_type · summary` plus the derived `category` and the underlying `EventRow` projection fields (`entity_id`, `source`, `status`, `duration_ms`, `agent_model`, `project_slug` when non-empty).

```sh
okt logs                                   # default window, every category
okt logs --category tool_call              # filter to CLI/MCP/TUI tool calls
okt logs --category task --category plan   # task + plan rows
okt logs --category task,plan              # equivalent comma form
okt logs --since 24h                       # narrow to the last 24 hours
okt logs --since 7d --limit 50             # last week, capped at 50 rows
```

> **Breaking change vs the legacy activity-log shape**: `okt logs` no longer returns rows shaped like `domain.ActivityLog`. Each row now carries the generic `event_type` + `summary` projection. Downstream tooling that scraped `source`-only rows should switch to the new shape.

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

Optional language flags mirror `okt setup`: `--cli-lang`, `--tui-lang`, and `--agent-lang`. Missing language flags prompt on an interactive TTY after the preset is seeded; in headless mode the seeded kit defaults remain. CLI/TUI codes are validated against the loaded language packs, while agent-output language is free-form.

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

### `okt config language` — inspect or update the language triple

`internal/cli/config_language.go`. Three subcommands manage the CLI / TUI / agent-output language triple captured at install time. All writes go through the bundle editor (atomic temp-file + rename).

- `okt config language show` — reports the resolved triple, the kit default, and each slot's source (default vs. user override).
- `okt config language set [--cli CODE] [--tui CODE] [--agent FREEFORM] [--global]` — updates one or more slots. CLI / TUI codes are validated against the loaded language packs (`internal/config/language.go::LoadLanguages`); unknown codes return `validation_error` with the list of available packs. The agent string is free-form (e.g. `"Português (Brasil)"`) because it is sent verbatim to the LLM. `--global` bypasses repo-local discovery and writes the user-global profile.
- `okt config language reset [--global]` — clears all three overrides so kit defaults apply again. No per-slot reset flag.

---

## Database

### `okt db backup [--out PATH] [--force]`

`internal/cli/db.go`. Writes a rolling snapshot of the resolved SQLite database with an atomic file copy (`tmp` + rename). Default destination is `$XDG_STATE_HOME/omakiten/backups/<timestamp>.db` (or `~/.local/state/omakiten/backups/`; under `$OMAKITEN_HOME`, `$OMAKITEN_HOME/state/backups/`). `--out` writes an exact destination path, creates parent directories with `0700`, skips retention pruning, and refuses common system roots such as `/etc` and `/usr`. `--force` allows an explicit `--out` path to overwrite an existing file.

The default JSON payload is `{path, pruned:true, retention}`. With `--out`, the payload is `{path, pruned:false}`.

```sh
okt db backup
okt db backup --out /mnt/external/omakiten-2026-05-24.db
okt db backup --out /mnt/external/omakiten-latest.db --force
```

Restoring is a manual operation today: stop any running `okt mcp serve` / `okt tui`, move the backup file in over the live `omakiten.db`, and restart. No `okt db restore` ships intentionally — restores are rare, destructive, and best done with eyes-on.

---

## Skills

File-backed under `skills/<slug>.md`. `internal/cli/skill.go`. Frontmatter: `name`, `description?` (`internal/config/entity_loader.go:skillFrontmatter`). Run `okt skill <subcommand> --help` for the full flag list.

> `SLUG` arguments accept either the slug **or** the numeric SQLite id (back-compat fallback in `internal/cli/editor.go:resolveSkillSlug`).

- `okt skill list` / `okt skill show SLUG` — no flags.
- `okt skill add` — `--name` required; `--key` defaults to `Slugify(--name)`. After creating the scaffold the command opens `$EDITOR` (then `$VISUAL`, then `nano`; resolved by `app.ResolveEditor`) and re-imports the bundle on save, unless `--no-edit` is set.
- `okt skill edit SLUG` — flag-driven `--name` / `--description` rewrites; `--no-edit` applies only flag-driven updates and skips `$EDITOR`.
- `okt skill remove SLUG` — deletes the file and prunes references in personas (`internal/app/skill_service.go`).

```sh
okt skill add -n "Go" -d "Idiomatic backend Go"
okt skill edit go --description "Idiomatic Go for CLIs and services"
okt skill remove go
```

---

## Laws

File-backed under `laws/<slug>.md`. `internal/cli/law.go`. Frontmatter: `name?`, `severity` (`info`/`warning`/`error`). Run `okt law <subcommand> --help` for the full flag list.

> `SLUG` arguments accept either the slug or the numeric id.

- `okt law list` — `--scope` filters by `global` / `project` / `persona`; pair with `--project SLUG` or `--persona SLUG` to narrow further.
- `okt law show SLUG` / `okt law remove SLUG` — no flags. Removal deletes the file and prunes references.
- `okt law add` — `--key` required; `--severity` defaults to `error`; `--scope` defaults to `global` and gates a `--project` or `--persona` slug when set to those values. Empty `--body` triggers a placeholder + `$EDITOR`; pass `--no-edit` to skip.
- `okt law edit SLUG` — flag-driven `--name` / `--severity` / `--body` rewrites; `--no-edit` applies only flag-driven updates.

```sh
okt law add -k workflow-enforced -n "Workflow Enforced" -s error
okt law list --scope persona --persona builder
```

---

## Personas

File-backed under `personas/<slug>.md`. `internal/cli/persona.go`. Wiring (skill refs) lives in the active profile yaml's `personas:` section. Run `okt persona <subcommand> --help` for the full flag list.

> `SLUG` arguments accept either the slug or the numeric id.

- `okt persona list` / `okt persona remove SLUG` — no flags.
- `okt persona show SLUG` — frontmatter, body, and resolved skill refs.
- `okt persona add` / `okt persona edit SLUG` — flag-driven rewrites. On `add`, `--name` is required and `--key` defaults to `Slugify(--name)`. Skill wiring uses either `--skill` / `-s` (numeric id, repeatable) or `--skill-slug` (slug, repeatable). On `edit`, the submitted skill set **replaces** the existing one entirely.

```sh
okt persona add -n "Builder" --skill-slug go --skill-slug sqlite
okt persona edit builder --skill-slug go --skill-slug cli   # replaces full skill set
```

---

## TUI

### `okt tui` — open the terminal UI

`internal/cli/tui.go`. Loads the active theme (`<root>/themes/<active>.yaml`, with `themes/custom/<active>.yaml` overriding) and starts Bubble Tea in alt-screen mode. Per-project navigation is hierarchical (`internal/tui/state.go`): three top zones — Tasks (board / table / graph), Stats (general / logs), Settings (general / laws / personas / skills / templates / tags) — plus a multi-project Home sentinel reachable via `0` or `ctrl+h`. The CLI plumbs the resolved active-profile path, SQLite path, and `okt` version into the TUI so Settings › General can surface them.

**Project-resolution behavior on launch:**

- With `--project` / `--project-id`, or when `$CWD` matches a registered project root, the TUI opens directly on that project's Board (existing behavior).
- Without any of the above, the TUI opens the Home Screen listing every registered project. Selecting one loads its Board normally. See [TUI Guide → Home](tui.md#home-multi-project-picker).

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

### `okt mcp prompts [name] [--list]`

Renders the resolved markdown for every `okt-*` MCP prompt, or one prompt when `name` is supplied. This is the CLI mirror of `prompts/get` and is useful for auditing persona/skill/law/template composition without starting an MCP client.

`--list` skips the bodies and prints the command-surface listing: the 40-command v2 kit grouped by routing tier (orchestrator / system / granular), with the granular tier sub-grouped by object namespace (`okt-<object>-<verb>`). It is the shell-side view of the MCP `prompts/list` surface — see `.docs/mcp.md#prompts` for the full tier breakdown.

### `okt mcp call TOOL_NAME --input JSON`

Calls a tool directly without going through the stdio server. Useful for scripting and testing. `--input` accepts a JSON object string (e.g. `'{"task_id":42}'`).

### `okt mcp serve`

Runs the JSON-RPC 2.0 stdio server (`internal/mcp/server.go:Serve`). No flags. Stdin/stdout are the transport — meant to be spawned by an MCP harness.

### `okt mcp setup`

Writes the `omakiten` MCP server entry into a harness config file (`internal/agentsetup/setup.go`). Mirrors the `--mcp-*` flags exposed by `okt init` but as standalone subcommand flags (`--harness`, `--config-path`, `--command`, `--dry-run`, `--force`). Supported harnesses: `claude-code` (default), `claude-desktop`, `opencode`, `crush`, `github-copilot`, `codex` (`internal/agentsetup/setup.go::SupportedHarnesses`). Run `okt mcp setup --help` for defaults.

```sh
okt mcp tools
okt mcp prompts okt-task-implement
okt mcp prompts --list
okt mcp call tasks.list --input '{"bucket_key":"dev"}'
okt mcp call search --input '{"query":"sqlite race","entity_types":["error","solution"]}'
okt mcp setup --harness opencode --dry-run
okt mcp serve   # invoked by the harness, not by hand
```

`okt mcp call search` is the CLI handle for the unified FTS5 surface (`internal/app/search_service.go`); it returns BM25-ranked hits with `<mark>...</mark>` snippets across tasks, comments, errors, solutions, and context entries. Pass `entity_types: []` (or omit the key) for an all-five sweep — the legacy `errors.search` MCP tool was retired alongside it.

The full set of MCP tools, resources, and prompts is documented in `.docs/mcp.md`.

---

## Output envelope

Every command writes to stdout one of:

```json
{"ok":true,"data": …}
```

```json
{"ok":false,"error":{"code":"<coded>","msg":"…","details":{…}}}
```

`code` is one of the constants in `internal/domain/errors.go` (e.g., `validation_error`, `task_not_found`, `workflow_invalid_transition`, `guard_violation`, `dependency_invalid`, `tag_conflict`, `editor_failed`, `config_invalid`). The full list and agent-side guidance is in `.docs/mcp.md` §"Failure Guidance".

A failed command exits with status `1`. JSON minification follows `config.output.json_minified`.

---

## See also

- [`configuration-guide/README.md`](./configuration-guide/README.md) — config keys CLI flags override.
- [`mcp.md`](./mcp.md) — agent equivalent of CLI commands.
- [`workflow.md`](./workflow.md) — preset CLI workflows.
- [`mcp.md`](./mcp.md) — agent equivalent for ops that live on MCP; CLI-only ops (`projects.delete`, `db.backup`, `update`, `uninstall`, `setup`) are documented in their respective subcommand sections above.
