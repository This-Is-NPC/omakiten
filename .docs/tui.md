# TUI Guide

`okt tui` opens the Bubble Tea terminal UI (`internal/cli/tui.go` → `internal/tui/model.go`). Navigation is hierarchical: three top-level **zones** (Tasks / Stats / Settings) plus a multi-project **Home** sentinel, each zone holding one or more **sub-menus**. Modal sub-screens (task detail, comment input, entity detail, pickers) layer on top of any sub.

## Contents

- [Home (multi-project picker)](#home-multi-project-picker)
- [Navigation model](#navigation-model)
- [Help overlay](#help-overlay)
- [Footer](#footer)
- [Per-zone keybindings](#per-zone-keybindings)
- [Modal sub-screens](#modal-sub-screens)
- [File-backed editing — the `$EDITOR` shellout](#file-backed-editing--the-editor-shellout)
- [Default sort, filter, and limits](#default-sort-filter-and-limits)
- [Live refresh](#live-refresh)
- [Scroll abstraction](#scroll-abstraction)
- [Theming](#theming)
- [Markdown rendering](#markdown-rendering)
- [See also](#see-also)

## Home (multi-project picker)

When `okt tui` is launched **outside** a registered project (no `--project` / `--project-id`, and the current working directory does not match any registered `root_path`), the TUI opens on the Home Screen. It lists every project in the local SQLite database as a card — name, slug, root path, pending task count, and the project's tags as filled-pill badges.

Home is **outside** the regular zone cycle. `tab` / `1`–`3` never land on Home. Entry points:

- starting `okt tui` without a resolvable project, or
- pressing `0` or `ctrl+h` from any per-project view.

While Home is active the per-project nav strip is suppressed (Home reads as a chromeless surface). Once a project is selected, the user's last (zone, sub) is preserved across the swap so Home behaves as a project switcher rather than a session reset.

### Home keybindings

| Key | Action |
|---|---|
| `↑ ↓` · `j k` | move project selection (auto-scrolls) |
| `pgup` · `pgdn` · `ctrl+u` · `ctrl+d` | scroll by half page |
| `g` · `G` | first / last project |
| `enter` | open the highlighted project (loads the Board) |
| `ctrl+h` | reload Home (refresh tags / pending counts) |
| `q` · `ctrl+c` | quit |

### `cd-on-exit` (parent shell follows the chosen project)

When you exit the TUI with a project loaded, `okt` writes the absolute root path of the most recently opened project to a small handshake file. The shell wrapper installed by `install.sh` / `install.ps1` reads that file and runs `cd "$path"` in the parent shell, so closing the TUI feels like having `cd`'d into the project.

The handshake-file path resolution mirrors what the wrapper expects:

1. `$OKT_CD_FILE` if set.
2. `$XDG_RUNTIME_DIR/okt-cd`.
3. `$TMPDIR/okt-cd-$UID` (or `/tmp/okt-cd-$UID` when `$TMPDIR` is unset).

If the wrapper is not installed (e.g. you run the bare `okt` binary in CI or via a script), the file is still written but nothing reads it — the TUI behaves identically and there is no error. To opt out, run the bundled `uninstall.sh` (or `uninstall.ps1`); it removes the wrapper block from your shell init using sentinel comments (`# >>> okt wrapper >>>` / `# <<< okt wrapper <<<`).

```sh
okt --project omakiten tui
```

The TUI consumes the same application services as the CLI and MCP layers — the SQLite store and bundled config files are the only state. There is no separate TUI cache.

## Navigation model

The header renders a single chrome with two rows of tiles:

```
00 // HOME │ 01 // TASKS   02 // STATS   03 // SETTINGS
                            ─────────
              // general    // logs
              ─────────
```

- **Top strip** lists `00 // HOME` (faded `│` divider) and the three zones. The active zone gets the accent rule beneath its tile.
- **Sub strip** lists the current zone's sub-menus; suppressed when the zone exposes a single sub.
- The breadcrumb above the strip reads `omakiten › <project-slug> · local checkpoint` (or `home · select a project` on Home). Long slugs are truncated to 40 chars so the layout stays intact at 80 cols.

### Zones and subs

| Top | Subs | Renderer family |
|---|---|---|
| `01 // TASKS` | `board` · `table` · `graph` · `plans` | `render_board.go`, `render_table.go`, `render_graph.go`, `render_plans.go`, `render_plan_network.go` |
| `02 // STATS` | `general` · `logs` | `render_stats.go`, `render_logs.go` |
| `03 // SETTINGS` | `general` · `laws` · `personas` · `skills` · `templates` · `tags` | `render_settings_general.go`, `render_config.go` (wraps `render_entity.go`) |
| `00 // HOME` (sentinel) | — | `render_home.go` |

Subs map to `subID` constants in `internal/tui/state.go`; the wiring (which dispatcher / renderer fires for which sub) lives in `Update` and `renderCurrentView` in `model.go` / `render_chrome.go`.

### Global keybindings

| Key | Action |
|---|---|
| `?` | open / close help overlay |
| `a` | toggle help between current context and all contexts |
| `q` · `ctrl+c` | quit |
| `tab` · `shift+tab` | cycle zones (sub lands on the new zone's first sub) |
| `1` · `2` · `3` | jump straight to Tasks / Stats / Settings |
| `,` · `/` | previous · next sub inside the active zone (no-op when only one sub) |
| `0` · `ctrl+h` | back to multi-project Home |
| `ctrl+o` | pop the back-stack (vim-style "older") to restore the previous (zone, sub) |
| `r` | refresh from store (per-zone semantics) |
| `ctrl+k` | open the [trick palette](#trick-palette) (Tricks tab + Search tab) — blocked while a modal input (comment / move / task / entity / help) owns the keyboard |

`left` / `right` (and `h` / `l`) are exclusively within-view bindings — Board lanes, Stats General period picker — and never switch zones. Cross-zone navigation is `tab` / digits / `,`/`/`.

### Back-stack (`ctrl+o`)

Every intentional navigation (`tab`, `shift+tab`, `1`/`2`/`3`, `,`/`/`, `0`, `ctrl+h`) pushes the current `(top, sub)` onto a session-scoped stack capped at 16 entries (`viewHistory` in `state.go`). `ctrl+o` pops the most recent entry. Empty-stack presses are silent no-ops. Refresh ticks and overlay close events do not touch the stack — it records *navigation*, not every state change. Persistence is intentionally session-only.

## Trick palette

`ctrl+k` opens the global trick palette overlay — a two-tab modal with **Tricks** (verb-prefixed shortcut commands like `nav:31`, `op:381`, `hook:1`) and **Search** (FTS5 fuzzy search with a navigable result list). The palette is mutually exclusive with the notification overlay: notifications suppress the palette panel until they dismiss, then the palette re-appears with prior state.

Full reference — command catalog, keybindings, configuration, hook recipes, troubleshooting — lives in [configuration-guide/tricks.md](configuration-guide/tricks.md).

## Help overlay

Press `?` from any view to open the keybindings overlay. By default it shows only the bindings relevant to the current context (current zone/sub plus any open overlay). Press `a` to expand to **all contexts** at once. The selection logic lives in `internal/tui/render_help.go:currentHelpTitles`. Group titles follow the `Tasks · board lens` / `Stats · general` / `Settings · entity (laws / personas / skills / templates)` shape so they read as zone-namespaced.

## Footer

Keybinding hints are emitted as structured `footerToken{key, label, primary}` records (`render_chrome.go:footerTokens`). The renderer:

- accents up to **three** tokens marked `primary: true` per surface in `hintAccent`; the rest stay in muted `hint`;
- guarantees `?` is the trailing token wherever help is reachable (`helpToken()` helper);
- standardises the verbal of Esc across overlays to `esc back` (`escBack()` helper); pickers and modal modes that *cancel* keep their own `esc cancel` because that action is destructive on save state.

The `primary` flag identifies the focal verb(s) of the surface (e.g. `enter open` / `n new` / `m move` on Board). Navigation tokens (`tab`, `,//`, `ctrl+o`) are never primary.

## Per-zone keybindings

### Common viewport bindings

Every scrollable surface in this guide — per-zone tables below and every modal sub-screen with a body or list — shares the same viewport keys. They are listed once here and omitted from the per-surface tables:

| Key | Action |
|---|---|
| `↑ ↓` · `j k` | move cursor / scroll (auto-scrolls when applicable) |
| `pgup` · `pgdn` · `ctrl+u` · `ctrl+d` | scroll by half page |
| `g` · `G` | jump to first / last |

`esc` is universal: read-style overlays use `esc back`; picker / mode-style overlays use `esc cancel` (destructive on save state). `r` refreshes the active view from the store wherever the zone is refreshable. Per-surface tables list only the verbs that distinguish each surface.

### Tasks › Board

| Key | Action |
|---|---|
| `← ↑ ↓ →` · `h l` | navigate lanes (column nav adds to common `j k`) |
| `enter` | open task (delete and edit live inside the task view) |
| `n` | new task |
| `e` | edit task |
| `c` | add comment |
| `m` | move task between lanes |
| `A` | toggle archived tasks (hidden by default; archived rows render dimmed) |

### Tasks › Table

| Key | Action |
|---|---|
| `enter` | open task (delete and edit live inside the task view) |
| `n` | new task |
| `e` | edit task |
| `m` | move by bucket key |
| `A` | toggle archived tasks (hidden by default) |

### Tasks › Graph

| Key | Action |
|---|---|
| `enter` | open task |

### Tasks › Plans

The fourth Tasks sub-tab surfaces WBS-style plans (`render_plans.go`, `render_plan_network.go`). It is the in-TUI counterpart to the MCP `plans.*` tools and the `okt plan ...` CLI surface.

The sub opens to a **list view** first; pressing `enter` on a plan opens the **network diagram** for that plan. `esc` returns to the list.

**List view** — one row per plan in the active project:

| Column | Source |
|---|---|
| slug | `plans.slug` |
| status | `active` / `done` / `abandoned` |
| done / total | child-task counts from `PlanService.ListRollups` |
| percent | `done_count / total_count` |
| name | `plans.name` |
| active wave | name of the first wave with unfinished tasks |

| Key | Action |
|---|---|
| `enter` | open network view for the selected plan |

Empty state renders a hint when the active project has no plans.

**Network diagram** — linear-cursor outline (one row per wave header + one row per task) rendered in a bordered table:

- Header line: `// PLAN · <slug> · done/total · percent` plus a keymap hint.
- Wave headers (`▼` expanded / `▶` collapsed) plus the `‹active›` marker on the first unfinished wave; every header anchors to the same screen column regardless of cross-wave filaments passing through.
- Tasks render under their wave as a `git log`-style rail (`├─` / `└─` / `│`) in DFS pre-order over intra-wave parents (input order preserved within sibling groups).
- Cross-wave dependencies surface as left-margin filaments — one lane per source task; the lane terminates with `└─►` on the destination row.
- Bordered table cells: `Title │ Bucket │ Deps`. Title flexes, Bucket is 10c, Deps is 14c (dropped on narrow terminals).
- Per-task inline state badge with semantic precedence: `done > gated > in-progress > blocked > assigned > ▶next > ready`. Glyphs: `✓` final-bucket · `⊘` gated · `○` default. `in-progress` = bucket between first and final (a workflow-shape fact, not derived from `assigned_to`).
- `@<assigned_to>` inline next to the task title when `tasks.assigned_to` is non-NULL.

| Key | Action |
|---|---|
| `h` · `l` | collapse / expand the wave under the cursor |
| `space` | toggle the wave under the cursor (on a task row, no-op) |
| `enter` · `o` | open the focused task; on a wave header, acts as `space` |
| `c` | open the single-line assignee editor for the focused task (see below) |
| `e` | open the in-TUI `goal_body` editor for the plan |
| `esc` · `q` | back to the list |

The `c` binding opens a single-line input pre-filled with the focused task's current assignee and calls `app.TaskService.Assign` on submit (empty input clears the assignee). The TUI explicitly does NOT auto-claim via `PlanService.ClaimNext`: bucket transitions still flow through `WorkflowService.MoveTask` so preset guards (e.g. omakase's self-branch-comment requirement on `backlog → dev`) remain authoritative. The TUI runs under `WithAgent("tui","tui","human","")` so the activity log distinguishes human assignments from agent claims.

`goal_body` edits go through an in-TUI textarea (consistent with the rest of the SQLite-backed entity surfaces); plans never shell out to `$EDITOR`.

### Stats › General

Per-AI-model benchmark over a configurable period (errors recorded, errors searched, search-before-record ratio, solutions added, like rate) plus the project's headline `Totals` (tasks / comments / context entries / tags) and `Tokens` (estimated / max + `[BUDGET EXCEEDED]` badge when truncated). The model-breakdown table reads from `app.MetricsService` — same aggregation the `metrics.summary` MCP tool returns.

| Key | Action |
|---|---|
| `←` · `→` | cycle period (`7d` → `30d` → `all`) |

The TUI itself reports `agent_model="human"` so its own activity does not appear in this benchmark — only MCP traffic with a real `_agent_model` does. See `.docs/mcp.md` for the underlying domain-event timeline.

### Stats › Logs

Unified event inspector — every `event_type` the project has recorded inside the snapshot's `views.logs.window_days` window renders through one 5-column row shape:

```
TIME · TYPE · ENTITY · WHO · DETAIL
```

| Column | Source | Notes |
|---|---|---|
| `TIME` | `events.created_at` | trailing 12 chars in the wide layout (HH:MM:SS in compact) |
| `TYPE` | `events.event_type` | painted via the `category.<name>` theme tokens (see below) |
| `ENTITY` | `entity_type + #entity_id` | collapses to `system` when `entity_id = 0` |
| `WHO` | `source` / `author_type` | tool calls show source, comments show author_type, system events show `—` |
| `DETAIL` | `domain.SummarizeEvent(row)` | per-event_type one-liner; never empty (see `internal/domain/event_summary.go`) |

Two bordered grid tables stack above the panel:

- **Categories** — one row per `domain.KnownEventCategories` entry with its window total from `EventRepository.EventCategoryCounts`. Every category is present (zero counts acceptable) so the grouping vocabulary is visible at a glance.
- **Health · tool_calls** — `ok` / `error` / `running` counts scoped explicitly to the `cli.tool_call` / `mcp.tool_call` / `tui.tool_call` / `hook.executed` subset. The kicker carries the scope so the numbers cannot be confused with the project-wide totals above.

Both tables aggregate over the same `views.logs.window_days` window the panel rows do.

Per-row TYPE coloring resolves through `category.<name>` theme tokens (`category.tasks`, `category.comment`, `category.plan`, `category.audit`, `category.guard`, `category.trick`, `category.tool_call`); themes that omit a token fall back to the neutral hint color so older / custom palettes keep rendering. The `tag-dep` and `hook` categories reuse the closest visual neighbor (`tasks` and `tool_call` respectively) to avoid a one-glyph palette per category.

The wide / compact split lives at the 92-cell `availableWidth()` threshold: terminals at or above render the full 5-column header; narrower terminals drop the explicit ENTITY / WHO column tags and fall back to `marker time type detail` — the `DETAIL` column still carries the per-row signal verbatim through `SummarizeEvent`, and TYPE keeps its category color so the per-row category remains readable without the dedicated column.

`Model.logsCategoryFilter` projects the active filter chip onto `EventFilter.Categories` — the refresh path reads it once per tick so the chip selection and the panel rows always stay aligned.

#### Filter chip (`F` cycle)

A single-line chip strip sits above the summary tables:

```
// LOGS · FILTER: [ all ] tool-calls domain system   (F cycle)
```

`F` rotates the active chip forward; `shift+F` rotates it backward. The active chip is bracketed and painted with the focus accent so the eye lands on it without colour-only signalling.

| Mode | Categories included | Event types rolled up |
|---|---|---|
| `all` | every `domain.KnownEventCategory` | no `Categories` filter passed to the repository |
| `tool-calls` | `tool_call`, `hook` | all CLI / MCP / TUI tool calls (`cli.tool_call`, `mcp.tool_call`, `tui.tool_call`) plus hook executions (`hook.executed`) |
| `domain` | `task`, `comment`, `plan`, `trick`, `tag-dep` | user-authored activity |
| `system` | `audit`, `guard`, `domain` | system bookkeeping |

Filter state lives on the Model so it survives the per-second realtime tick, manual `r` refreshes, and zone re-entries — the user picks the chip once and the surface stays scoped until the next `F` press.

| Key | Action |
|---|---|
| `F` | cycle filter chip forward (`all → tool-calls → domain → system → all`) |
| `shift+F` | cycle filter chip backward |

The TUI's per-second realtime tick is **not** logged — `refreshTickMsg` wraps `m.ctx` with `activity.WithoutTracking` before calling `refreshCurrentView`, so the tick's `MetricsService.Summary` / list calls bypass `activity.Track`. Only user-explicit refreshes (`r`), refreshes after a view change, and writes from the application services land in the log.

### Settings › General (read-only)

Runtime info card with two stacked bordered tables:

- **Runtime**: `okt version`, active profile yaml path, SQLite path
- **Project**: active workflow key, bucket keys, active theme

Mutating any of these still goes through dedicated pickers (`t` for theme, `c` for config) which remain reachable from every Settings sub.

| Key | Action |
|---|---|
| `t` | open theme picker (hot-reload) |
| `c` | open config picker (hot-reload) |

#### Config picker hot-reload

Selecting a profile re-imports the new bundle in place: the editor repoints
at the chosen yaml, the `EnumRegistry`, workflow service, theme/styles/
markdown, notification catalog, and token-badge thresholds all refresh
without restarting the program. `paths.SetActiveConfig` is written only after
the import succeeds, so a validator rejection on the new bundle keeps the
on-disk `.active` pointing at the previous (working) profile.

When the new workflow drops bucket keys the previous one had, tasks in those
buckets become orphaned. After the swap commits the TUI emits `bundle.swapped`
with the orphan preview folded into the payload (`orphan_count`, `has_orphans`,
`groups`). The hooks engine matches a `when: { has_orphans: "true" }` filter
and paints the `kitten_orphan_migration` notification:

- `m` Migrate — dispatches `okt workflow orphans --confirm` in-process; tasks
  rebind to the matching key in the new workflow (preserved) or to the first
  active bucket (removed).
- `s` Skip — dismisses without side effect; tasks stay orphaned until the
  user runs the CLI command later.
- `esc` Cancel — reverts the swap by re-importing the previous bundle and
  rewriting `.active`. No commit happens until the user explicitly chooses
  Migrate or Skip.

Every dispatched action emits `confirmation.granted` with `author_type=human`
keyed by the active project so the audit log captures the human keystroke
that authorised the run.

### Settings › Laws / Personas / Skills

Each entity kind owns its own sub. Cards wrap into a multi-column grid sized to the available terminal width (`entityGridCols`); per-row scroll keeps the focused card on-screen.

| Key | Action |
|---|---|
| `enter` | open detail |
| `n` | new entity (creates the file and shells out to `$EDITOR`) |
| `e` | edit in `$EDITOR` |
| `d` `d` | arm delete, then confirm (two-press fuse) |
| `p` | open the skill picker (when viewing a persona) |
| `t` · `c` | theme picker · config picker |

### Settings › Templates

Templates are auto-loaded from `<config-root>/templates/` so `n` and `d` are no-ops with a hint; you add or remove templates by editing files on disk and refreshing.

| Key | Action |
|---|---|
| `enter` | open detail |
| `a` | open the default-assignment picker (which kind this template is the default for) |
| `t` · `c` | theme picker · config picker |

### Settings › Tags

Tag browser. Only **orphan** tags (zero references) can be deleted from the TUI; tags with non-zero usage cannot be removed without first detaching their references.

| Key | Action |
|---|---|
| `d` | arm delete (orphan only) |
| `D` | delete every orphan tag (one shot) |
| `t` · `c` | theme picker · config picker |

## Modal sub-screens

These open on top of a zone/sub and intercept all input until dismissed. The contextual help overlay (`?`) automatically narrows to whichever sub-screen is open. Every scrollable modal inherits [Common viewport bindings](#common-viewport-bindings); per-modal tables list only the surface-specific verbs.

### Modal text inputs

Every modal that captures text — `modeMove`, the inline new-comment (`modeComment`), the dedicated comment edit overlay (`commentScreenEditing`), and the create / edit task form — drives a Charm `bubbles` component: `textinput.Model` for single-line surfaces (`modeMove`, task title), `textarea.Model` for multi-line surfaces (comments, task description). Standard caret keys (`↑ ↓ ← →` · `home` · `end`) apply throughout.

`KeyMap.InsertNewline` is rebound on every textarea so `shift+enter` / `alt+enter` / `ctrl+j` insert a newline natively. For the task description textarea — where `ctrl+s` is the save key and a bare Enter is free for newlines — the binding includes `enter` too. For the comment textareas, `enter` is reserved for "save". The per-modal tables below therefore omit caret/newline rows and list only surface-specific verbs.

`internal/tui/keys.go` declares the `commentInputBindings` and `moveInputBindings` records via `bubbles/key.Binding`. The same records drive both the runtime handlers in `updateInput` and the help-overlay rows in `render_help.go` — there is no separate hard-coded help string.

**Cursor visibility:** every `bubbles` input renders its cursor with `Cursor.Style = m.styles.cursor` (foreground = primary accent) so the reverse-pass produces a visible primary-colored block regardless of the surrounding line styling. Textareas additionally clear `FocusedStyle.CursorLine.Background` (`clearTextareaCursorLineBackground` in `model.go`) so the line background no longer swallows the cursor cell.

### Task view (after `enter` on a task card)

Destructive verbs live inside the entered surface only — the board has no `d` shortcut. Pressing `e` or `d` runs a policy pre-check; if the bucket forbids it the guard hint surfaces in the status badge instead of opening the form. `↑ ↓` · `j k` route to the description (form focus) or activity cards (activity focus); `esc back` returns to the launching surface.

| Key | Action |
|---|---|
| `tab` · `shift+tab` | switch focus (form ⇄ sub-tasks ⇄ activity) |
| `J` · `K` | navigate activity cards regardless of focus |
| `enter` | open the focused comment in the comment-detail view |
| `e` | edit task (form-column focus only; gated by `permissions.task.edit`) |
| `b` | edit blockers (opens the blocker picker) |
| `c` | add comment |
| `m` | move — sub-tasks focus targets the focused child, every other focus zone targets the open task; prompt label appends the resolved kit's bucket keys (e.g. `Target bucket — backlog · dev · done`) so valid targets are visible without leaving the input |
| `M` | toggle markdown render of the description (raw ⇄ rendered; default rendered) |
| `d` `d` | arm hard-delete the task, then confirm (form-column focus only; gated by `permissions.task.delete` and `operations.delete.guards`) |

### Sub-tasks panel (inside Task view)

The task view always renders three panels — task details on the left, sub-tasks and activity stacked on the right (side-by-side layout) or cascaded below the form (stacked layout). The sub-tasks panel lists the direct children of the focused task; pressing `enter` on a child drills into it, pushing the current task onto an ancestor stack so `esc` unwinds one level at a time. `j`/`k` route to whichever column owns focus; `J`/`K` always reach the activity feed.

| Key | Action |
|---|---|
| `tab` · `shift+tab` | switch focus (form ⇄ sub-tasks ⇄ activity) |
| `a` · `n` | add sub-task (opens the task form pre-attached to the current task as parent) |
| `s` | focus the sub-tasks panel (no-op with status hint when the task has no children) |
| `space` | send the focused child straight to the workflow's final bucket — resolves the final bucket from the child's resolved kit so sub-tasks land in the sub-kit's terminal bucket, not the root kit's (guards still fire; errors surface inline) |
| `m` | move the focused child by bucket key; the input prompt appends the sub-kit's bucket keys so valid targets are visible inline |
| `enter` | drill into the focused child (becomes the new task view; `esc` pops back) |
| `f` | open the dedicated description overlay for the focused task |

`f` is wired to the **parent task's** description, not the focused child — it surfaces the body when the inline form column truncates after `taskDescriptionInlineCap` lines (6 by default; the elision hint reads `+N more · f to focus`). To read a child's description, `enter` into it first.

The `Parent` field on the task form (§E, last in the `Title → Description → Priority → Tags → Parent` rotation) holds the parent task id as a decimal — empty means root. The field is pre-filled from `tasks.parent_id` on edit, captured into `taskEditInitial` so `esc` can detect a dirty edit without re-querying the store, and validated on blur via `validateParentInputOnBlur` (rotation through `cycleTaskField`). Creating a sub-task through `a` / `n` instead routes through `TaskService.AddSub` with `taskCreateParentID` held out-of-band and rendered as a breadcrumb in the form header.

### Comment view (after `enter` on a comment)

| Key | Action |
|---|---|
| `e` | edit comment body (gated by `permissions.comment.edit`) |
| `M` | toggle markdown render of the comment body (raw ⇄ rendered; default rendered) |
| `d` `d` | arm delete the comment, then confirm (gated by `permissions.comment.delete`) |

### Comment input — new comment (after `c` on a task card or in task view)

A multi-line `bubbles/textarea` opens **inline inside the activity column** of the task view; the body is empty and the caret is focused. `enter` saves; `esc` cancels and returns to whichever surface launched the modal.

| Key | Action |
|---|---|
| `enter` | save (creates a new comment) |
| `esc` | cancel |

### Comment edit — edit existing comment (`e` on the comment view)

Pressing `e` on the comment-view overlay flips the **same overlay** into a dedicated full-screen edit form (kicker · hint · bordered textarea · footer) — distinct from the inline new-comment input. The pre-filled body, the wider textarea, and `ctrl+s` as the save key match the task-edit form so the two write surfaces feel uniform.

| Key | Action |
|---|---|
| `ctrl+s` | save the rewritten body (gated by `permissions.comment.edit`; tags survive the round-trip) |
| `esc` | cancel — return to the read-only comment view |

### Task form (`n` to create, `e` to edit)

In the description textarea, bare `enter` also inserts a newline (the textarea is the only one where `ctrl+s`, not `enter`, is the save key).

| Key | Action |
|---|---|
| `tab` | switch field |
| `← →` · `h l` | change priority |
| `ctrl+b` | edit blockers (existing tasks only) |
| `ctrl+s` | save |
| `esc` | cancel |

### Blocker picker (from task view via `b`, or from the form via `ctrl+b`)

| Key | Action |
|---|---|
| `space` | toggle blocker |
| `ctrl+s` | save |
| `esc` | cancel |

### Entity view (Settings › entity → `enter`)

`esc` here also cancels a pending delete.

| Key | Action |
|---|---|
| `e` | edit (opens `$EDITOR`) |
| `M` | toggle markdown render of the entity body (raw ⇄ rendered; default rendered) |
| `d` `d` | arm delete, then confirm |
| `p` | skill picker (persona only) |

### Skill picker (from a persona via `p`)

| Key | Action |
|---|---|
| `space` | toggle |
| `enter` on `+ create new` | scaffold new skill |
| `ctrl+s` | save |
| `esc` | cancel |

### Theme / config / template-default pickers

| Key | Action |
|---|---|
| `enter` | apply (theme: hot-reload; config: hot-reload via `BundleCache.Reload`; default: clears prior owner) |
| `esc` | cancel |

## File-backed editing — the `$EDITOR` shellout

For skills, laws, personas, and templates, "new" and "edit" actions in **Settings** shell out to the resolved editor (`$EDITOR` → `$VISUAL` → `nano`, in that order; `internal/app/editor.go:ResolveEditor`). When the editor exits successfully, the bundle is re-imported through `app.BundleEditor.Apply` so the in-memory `*config.Snapshot` (and the per-project `ProjectRuntime` cache entry) reflect the on-disk change — migration 020 removed the SQL config mirror, so no rows are written by the reimport beyond the `bundle.imported` audit event.

Two consequences worth knowing:

- The TUI is paused while the editor runs. After save, the entity view re-renders with the new content.
- If the YAML/frontmatter is invalid, the re-import fails and the error is surfaced as a coded error in the entity view header — fix the file and re-open.

## Default sort, filter, and limits

Per-view defaults come from `config.views` in the active profile yaml (`Settings.EffectiveViews()`). The TUI seeds itself from these on startup. Allowed values and canonical defaults are documented in `.docs/configuration-guide/README.md` §"`config.views`".

## Live refresh

Per-project zones emit a refresh tick about once a second while idle (`internal/tui/state.go:refreshTickMsg`). Editing, an open modal, or sitting on Home suppresses the tick — see `Model.shouldRealtimeRefresh`. The tick wraps its context with `activity.WithoutTracking` so its app-service calls do not pollute the activity log; `r` and refreshes after a view change still track.

## Scroll abstraction

Every list / grid / line viewport in the TUI flows through a single algorithm in `internal/tui/components/scrollwindow/`. The package is a leaf with no dependency on the parent `tui` package, so detail-screen sub-components can import it without an import cycle.

### `scrollwindow` — pure scroll math

```go
type HintMode int
const (
    HintsSplit    HintMode = iota // ▲ above + ▼ below as separate rows
    HintsCombined                  // single combined ▲ X · ▼ Y footer
    HintsNone                      // caller handles indicator chrome outside
)

func Slice(offset int, heights []int, viewport int, mode HintMode) int
func Follow(offset, cursor int, heights []int, viewport int, mode HintMode) int
```

`Slice` returns the visible end index given offset, per-item heights (in terminal rows), and a viewport row budget. It reserves rows for indicator chrome dynamically based on the chosen `HintMode` and the actual scroll position, so the rendered slice plus its hint rows can never exceed `viewport`. `Follow` is the cursor-sync analog used by per-frame routines that keep the cursor on screen.

Heights of `1` service fixed-height surfaces (table rows, log entries, picker rows, activity feed lines); measured heights service variable-height surfaces (board cards, entity grid rows, home project cards). Same helper, parameterized contract — fixed-height is just a special case where every item happens to take one terminal row.

### Assembly helpers (in `internal/tui/scroll.go`)

- `m.renderScrollWindowSplit(items, heights, offset, viewport)` — wraps `scrollwindow.Slice` with `HintsSplit`, prepends `▲ N above` and appends `▼ N below` rows. Used by the board lanes, settings entity grid, home projects column, activity feed, and (via `sliceScrollRows`) the fixed-height table/logs/graph/picker surfaces.
- `followScrollWindowSplit(offset, cursor, heights, viewport)` — sync analog. Used by `syncFocusedColumnScroll`, `syncFocusedEntityScroll`, and `syncHomeScroll`.
- `m.sliceScrollRows(rows, scroll, viewport)` — public API for fixed-height callers. Thin wrapper that builds heights of 1s and delegates to `renderScrollWindowSplit`.
- `m.panelViewportRows(panelChrome int)` — terminal-row budget for any panel sitting under the screen chrome. Live-measures the screen header / status line / footer so the budget tracks header changes automatically. Each caller declares only the rows internal to its own panel (border + kicker + separator + any trailing hint).
- `scrollDataRows(viewport)` — the cursor-tracking adapter for fixed-height surfaces; subtracts the 2 worst-case hint rows so cursor + scroll math agree.
- `followCursor(scroll, cursor, viewport, total)` — fixed-height cursor follow used by every picker.

### Detail-screen viewport (combined-footer style)

The task view, comment view, help overlay, and entity view emit a single combined `▲ X above · ▼ Y below · j/k pgup/pgdn g/G` footer instead of split hint rows. They route through the same `scrollwindow.Slice` math but with `HintsNone` — the caller passes `viewport-1` and renders the footer outside the slice budget. See `internal/tui/components/viewport/viewport.go` for the assembly and `internal/tui/viewport.go:sliceViewport` for the parent-package wrapper.

### Adding a new scrollable surface

1. Compute heights — `1` per item for fixed-height, `strings.Count(rendered, "\n") + 1` for measured.
2. Get a viewport budget via `m.panelViewportRows(panelChrome)`.
3. For split-hint UX: assemble via `m.renderScrollWindowSplit(items, heights, offset, viewport)` and sync cursor via `followScrollWindowSplit(...)`.
4. For combined-footer UX: route through `components/viewport.Model.View(lines, viewport-1, hintStyle)`.

Never inline the walk-and-reserve loop. The 16-case `scrollwindow_test.go` locks the contract; future bugs in scroll budgeting land in one place.

## Theming

The TUI loads its theme from `<config-root>/themes/<active>.yaml` with `themes/custom/<active>.yaml` taking precedence (`internal/cli/tui.go:loadActiveTheme`). The active theme key is `config.theme.active`. See `.docs/configuration-guide/themes.md` for color tokens and authoring.

## Markdown rendering

Body fields shown in the read-only detail panels (task description, comment body inside the dedicated comment view, and the entity body for laws / personas / skills / templates) render as styled markdown by default. The renderer (`internal/tui/markdown.go`) builds an `ansi.StyleConfig` from the active theme tokens (`primary`, `foreground`, `border`, `secondary`) — switching theme rebuilds the renderer and clears its per-(body, width) cache. Code blocks render as plain mono (no chroma syntax highlight) so they stay aligned with the dev-editorial palette.

Press `M` (capital) inside the task view, comment view, or entity view to toggle between raw and rendered for the rest of the session — useful when you need to copy markdown verbatim or debug formatting. The toggle is session-only; it is not persisted to the active profile yaml. The status badge confirms the active mode (`Markdown rendered` / `Markdown raw`). Editing flows (textareas + `$EDITOR`) are unaffected — they always show raw text.

## See also

- [`configuration-guide/themes.md`](configuration-guide/themes.md) — color tokens used by the TUI.
- [`configuration-guide/README.md`](configuration-guide/README.md) — TUI scope, layout config.
- [`cli.md`](cli.md) — sibling CLI surface.
- [`mcp.md`](mcp.md) — agent surface mirror of TUI ops.
