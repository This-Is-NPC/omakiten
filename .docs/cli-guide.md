# CLI Commands (`okt`)

`okt` is the CLI entrypoint for Omakiten (`cmd/okt/main.go` → `internal/cli/root.go:NewRootCommand`). Every command emits a single JSON envelope (`{"ok": true, "data": …}` on success, `{"ok": false, "error": {…}}` on failure) via `internal/output/json.go`. Failures carry the coded error from `internal/domain/errors.go`; the process exits with code `1`.

> All flag tables below are derived from source in `internal/cli/`. The shipped binary's `--help` may lag if it is older than the current source — when in doubt, the source is canonical.

## Global flags

Inherited by every subcommand (`internal/cli/root.go:NewRootCommand`):

| Flag | Type | Default | Effect |
|---|---|---|---|
| `--config` | string | resolved | Override path to `omakiten.yaml`. Highest precedence over `$OMAKITEN_HOME` and XDG. |
| `--db` | string | resolved | Override path to the SQLite database. |
| `--project`, `-p` | string | — | Active project slug. |
| `--project-id` | int | — | Active project id (numeric). |
| `--help`, `-h` | — | — | Help for the subcommand. |
| `--version`, `-v` | — | — | Print the binary version (root only). |

**Project resolution order** (`internal/project/resolver.go`): `--project-id` → `--project` → current working directory matching a registered project root.

**Path resolution order** (`internal/paths/paths.go`): `--config`/`--db` flag → `$OMAKITEN_HOME` → `$XDG_CONFIG_HOME`/`$XDG_DATA_HOME` → `~/.config/omakiten` and `~/.local/share/omakiten`.

---

## `okt init` — register the current project

`internal/cli/init.go`. Inserts a project row in the global SQLite DB (`UpsertProject`); optionally writes the MCP harness config.

| Flag | Default | Effect |
|---|---|---|
| `--name` | — | Project display name. |
| `--slug` | — | Project slug (kebab-case key). |
| `--root` | `$CWD` | Project root path used for CWD-based resolution. |
| `--enable-mcp` | `false` | Also configure an MCP harness entry. |
| `--mcp-harness` | `claude-code` | One of `claude-code`, `claude-desktop`, `opencode` (`internal/agentsetup/setup.go`). |
| `--mcp-config` | harness default | Override the harness config-file path. |
| `--mcp-command` | current executable | Command written into the harness config. |
| `--mcp-dry-run` | `false` | Preview changes without writing. |
| `--mcp-force` | `false` | Replace an existing `mcpServers.omakiten` entry. |

```sh
okt init --name Omakiten --slug omakiten --root "$PWD"
okt init --slug acme --enable-mcp --mcp-harness opencode --mcp-dry-run
```

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
| `--priority` | `low`, `normal`, or `high` (`internal/config/validator.go:allowedPriorities`). |
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

```sh
okt comment add 42 -b "Branch: feature/foo" -T self-branch
okt comment list 42
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

---

## Config

`internal/cli/config.go`.

### `okt config validate [path]`

Runs `config.ValidateBundle` against the supplied YAML (or the resolved one). Reports the first failing rule from `internal/config/validator.go`.

```sh
okt config validate
okt config validate ./omakiten.yaml
```

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

File-backed under `personas/<slug>.md`. `internal/cli/persona.go`. Wiring (skill refs) lives in `omakiten.yaml`'s `personas:` section.

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

`internal/cli/tui.go`. Loads the active theme (`<root>/themes/<active>.yaml`, with `themes/custom/<active>.yaml` overriding) and starts Bubble Tea in alt-screen mode. Five per-project views (BOARD, TABLE, GRAPH, CONFIG, LOGS — `internal/tui/state.go:viewNames`) plus a multi-project Home reachable via `ctrl+h`.

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
| `--harness` | `claude-code` | One of `claude-code`, `claude-desktop`, `opencode`. |
| `--config-path` | harness default | Override the harness config-file path. |
| `--command` | current executable | Command written into the harness config. |
| `--dry-run` | `false` | Preview changes without writing. |
| `--force` | `false` | Replace an existing entry. |

```sh
okt mcp tools
okt mcp call tasks.list --input '{"bucket_key":"dev"}'
okt mcp setup --harness opencode --dry-run
okt mcp serve   # invoked by the harness, not by hand
```

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
