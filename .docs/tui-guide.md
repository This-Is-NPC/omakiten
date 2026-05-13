# TUI Guide

`okt tui` opens the Bubble Tea terminal UI (`internal/cli/tui.go` → `internal/tui/model.go`). Navigation is hierarchical: three top-level **zones** (Tasks / Stats / Settings) plus a multi-project **Home** sentinel, each zone holding one or more **sub-menus**. Modal sub-screens (task detail, comment input, entity detail, pickers) layer on top of any sub.

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
| `01 // TASKS` | `board` · `table` · `graph` | `render_board.go`, `render_table.go`, `render_graph.go` |
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

`left` / `right` (and `h` / `l`) are exclusively within-view bindings — Board lanes, Stats General period picker — and never switch zones. Cross-zone navigation is `tab` / digits / `,`/`/`.

### Back-stack (`ctrl+o`)

Every intentional navigation (`tab`, `shift+tab`, `1`/`2`/`3`, `,`/`/`, `0`, `ctrl+h`) pushes the current `(top, sub)` onto a session-scoped stack capped at 16 entries (`viewHistory` in `state.go`). `ctrl+o` pops the most recent entry. Empty-stack presses are silent no-ops. Refresh ticks and overlay close events do not touch the stack — it records *navigation*, not every state change. Persistence is intentionally session-only.

## Help overlay

Press `?` from any view to open the keybindings overlay. By default it shows only the bindings relevant to the current context (current zone/sub plus any open overlay). Press `a` to expand to **all contexts** at once. The selection logic lives in `internal/tui/render_help.go:currentHelpTitles`. Group titles follow the `Tasks · board lens` / `Stats · general` / `Settings · entity (laws / personas / skills / templates)` shape so they read as zone-namespaced.

## Footer

Keybinding hints are emitted as structured `footerToken{key, label, primary}` records (`render_chrome.go:footerTokens`). The renderer:

- accents up to **three** tokens marked `primary: true` per surface in `hintAccent`; the rest stay in muted `hint`;
- guarantees `?` is the trailing token wherever help is reachable (`helpToken()` helper);
- standardises the verbal of Esc across overlays to `esc back` (`escBack()` helper); pickers and modal modes that *cancel* keep their own `esc cancel` because that action is destructive on save state.

The `primary` flag identifies the focal verb(s) of the surface (e.g. `enter open` / `n new` / `m move` on Board). Navigation tokens (`tab`, `,//`, `ctrl+o`) are never primary.

## Per-zone keybindings

### Tasks › Board

| Key | Action |
|---|---|
| `← ↑ ↓ →` · `h j k l` | navigate lanes and tasks (auto-scrolls column) |
| `pgup` · `pgdn` · `ctrl+u` · `ctrl+d` | scroll focused column by page |
| `g` · `G` | first / last card in column |
| `enter` | open task (delete and edit live inside the task view) |
| `n` | new task |
| `e` | edit task |
| `c` | add comment |
| `m` | move task between lanes |
| `A` | toggle archived tasks (hidden by default; archived rows render dimmed) |

### Tasks › Table

| Key | Action |
|---|---|
| `↑ ↓` · `j k` | select task (auto-scrolls) |
| `pgup` · `pgdn` · `ctrl+u` · `ctrl+d` | scroll by half page |
| `g` · `G` | first / last task |
| `enter` | open task (delete and edit live inside the task view) |
| `n` | new task |
| `e` | edit task |
| `m` | move by bucket key |
| `A` | toggle archived tasks (hidden by default) |

### Tasks › Graph

| Key | Action |
|---|---|
| `↑ ↓` · `j k` | move cursor |
| `enter` | open task |
| `pgup` · `pgdn` · `ctrl+u` · `ctrl+d` | scroll by half page |
| `g` · `G` | jump to top / bottom |

### Stats › General

Per-AI-model benchmark over a configurable period (errors recorded, errors searched, search-before-record ratio, solutions added, like rate) plus the project's headline `Totals` (tasks / comments / context entries / tags) and `Tokens` (estimated / max + `[BUDGET EXCEEDED]` badge when truncated). The model-breakdown table reads from `app.MetricsService` — same aggregation the `metrics.summary` MCP tool returns.

| Key | Action |
|---|---|
| `←` · `→` | cycle period (`7d` → `30d` → `all`) |
| `r` | refresh |

The TUI itself reports `agent_model="human"` so its own activity does not appear in this benchmark — only MCP traffic with a real `_agent_model` does. See `.docs/mcp-guide.md` for the underlying domain-event timeline.

### Stats › Logs

Activity log viewer. Two bordered grid tables stack above the panel (Status: total / ok / error / running, Sources: cli / mcp / tui) — both **aggregate the entire project history** via `ActivityLogStats`, regardless of how many rows the panel beneath happens to render under `views.logs.limit`. The panel itself is paged by limit and ordered DESC by `created_at`, with the user-configured source filter applied (`views.logs.filter.source`).

| Key | Action |
|---|---|
| `↑ ↓` · `j k` | select row (auto-scrolls) |
| `pgup` · `pgdn` · `ctrl+u` · `ctrl+d` | scroll by half page |
| `g` · `G` | first / last row |
| `r` | refresh |

The TUI's per-second realtime tick is **not** logged — `refreshTickMsg` wraps `m.ctx` with `activity.WithoutTracking` before calling `refreshCurrentView`, so the tick's `MetricsService.Summary` / list calls bypass `activity.Track`. Only user-explicit refreshes (`r`), refreshes after a view change, and writes from the application services land in the log.

### Settings › General (read-only)

Runtime info card with two stacked bordered tables:

- **Runtime**: `okt version`, active profile yaml path, SQLite path
- **Project**: active workflow key, bucket keys, active theme

Mutating any of these still goes through dedicated pickers (`t` for theme, `c` for config) which remain reachable from every Settings sub.

| Key | Action |
|---|---|
| `t` | open theme picker (hot-reload) |
| `c` | open config picker (restart required) |
| `r` | refresh |

### Settings › Laws / Personas / Skills

Each entity kind owns its own sub. Cards wrap into a multi-column grid sized to the available terminal width (`entityGridCols`); per-row scroll keeps the focused card on-screen.

| Key | Action |
|---|---|
| `↑ ↓` · `j k` | select entity (auto-scrolls) |
| `pgup` · `pgdn` · `ctrl+u` · `ctrl+d` | scroll by half page |
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
| `↑ ↓` · `j k` | select template |
| `enter` | open detail |
| `a` | open the default-assignment picker (which kind this template is the default for) |
| `t` · `c` | theme picker · config picker |

### Settings › Tags

Tag browser. Only **orphan** tags (zero references) can be deleted from the TUI; tags with non-zero usage cannot be removed without first detaching their references.

| Key | Action |
|---|---|
| `↑ ↓` · `j k` | select tag |
| `d` | arm delete (orphan only) |
| `D` | delete every orphan tag (one shot) |
| `t` · `c` | theme picker · config picker |

## Modal sub-screens

These open on top of a zone/sub and intercept all input until dismissed. The contextual help overlay (`?`) automatically narrows to whichever sub-screen is open. Esc verbal across all back-style overlays is `esc back`; cancel-style modals (pickers, modes) keep `esc cancel`.

### Modal text inputs

Every modal that captures text — `modeMove` (move task by bucket key), the inline new-comment (`modeComment`), the dedicated comment edit overlay (`commentScreenEditing`), and the create / edit task form — drives a Charm `bubbles` component:

- `bubbles/textinput.Model` — single-line surfaces (`modeMove`, task title field).
- `bubbles/textarea.Model` — multi-line surfaces (comment add, comment edit, task description).

`KeyMap.InsertNewline` is rebound on every textarea so `shift+enter` / `alt+enter` / `ctrl+j` insert a newline natively (the prior `isNewlineModifier` shim that injected `\n` via `InsertString` is gone). For the task description textarea — where `ctrl+s` is the save key and a bare Enter is free for newlines — the binding includes `enter` too. For the comment textareas, `enter` is reserved for "save".

`internal/tui/keys.go` declares the `commentInputBindings` and `moveInputBindings` records via `bubbles/key.Binding`. The same records drive both the runtime handlers in `updateInput` and the help-overlay rows in `render_help.go` — there is no separate hard-coded help string.

**Cursor visibility:** every `bubbles` input renders its cursor with `Cursor.Style = m.styles.cursor` (foreground = primary accent) so the reverse-pass produces a visible primary-colored block regardless of the surrounding line styling. Textareas additionally clear `FocusedStyle.CursorLine.Background` (`clearTextareaCursorLineBackground` in `model.go`) so the line background no longer swallows the cursor cell.

### Task view (after `enter` on a task card)

Destructive verbs live inside the entered surface only — the board has no `d` shortcut. Pressing `e` or `d` runs a policy pre-check; if the bucket forbids it the guard hint surfaces in the status badge instead of opening the form.

| Key | Action |
|---|---|
| `tab` · `shift+tab` | switch focus (form ⇄ activity column) |
| `↑ ↓` · `j k` | scroll description (form focus) · navigate cards (activity focus) |
| `J` · `K` | navigate activity cards regardless of focus |
| `enter` | open the focused comment in the comment-detail view |
| `pgup` · `pgdn` · `ctrl+u` · `ctrl+d` | scroll by half page |
| `g` · `G` | jump to top / bottom |
| `e` | edit task (form-column focus only; gated by `permissions.task.edit`) |
| `b` | edit blockers (opens the blocker picker) |
| `c` | add comment |
| `m` | move |
| `M` | toggle markdown render of the description (raw ⇄ rendered; default rendered) |
| `d` `d` | arm hard-delete the task, then confirm (form-column focus only; gated by `permissions.task.delete` and `operations.delete.guards`) |
| `esc` | back |

### Comment view (after `enter` on a comment)

| Key | Action |
|---|---|
| `↑ ↓` · `j k` | scroll body |
| `pgup` · `pgdn` · `ctrl+u` · `ctrl+d` | scroll by half page |
| `g` · `G` | jump to top / bottom |
| `e` | edit comment body (gated by `permissions.comment.edit`) |
| `M` | toggle markdown render of the comment body (raw ⇄ rendered; default rendered) |
| `d` `d` | arm delete the comment, then confirm (gated by `permissions.comment.delete`) |
| `esc` | back |

### Comment input — new comment (after `c` on a task card or in task view)

A multi-line `bubbles/textarea` opens **inline inside the activity column** of the task view; the body is empty and the caret is focused. `enter` saves; `esc` cancels and returns to whichever surface launched the modal.

| Key | Action |
|---|---|
| `enter` | save (creates a new comment) |
| `alt+enter` · `shift+enter` · `ctrl+j` | insert newline |
| `↑ ↓ ← →` · `home` · `end` | caret navigation within the body |
| `esc` | cancel |

### Comment edit — edit existing comment (`e` on the comment view)

Pressing `e` on the comment-view overlay flips the **same overlay** into a dedicated full-screen edit form (kicker · hint · bordered textarea · footer) — distinct from the inline new-comment input. The pre-filled body, the wider textarea, and `ctrl+s` as the save key match the task-edit form so the two write surfaces feel uniform.

| Key | Action |
|---|---|
| `ctrl+s` | save the rewritten body (gated by `permissions.comment.edit`; tags survive the round-trip) |
| `alt+enter` · `shift+enter` · `ctrl+j` | insert newline |
| `↑ ↓ ← →` · `home` · `end` | caret navigation within the body |
| `esc` | cancel — return to the read-only comment view |

### Task form (`n` to create, `e` to edit)

| Key | Action |
|---|---|
| `tab` | switch field |
| `← →` · `h l` | change priority |
| `ctrl+b` | edit blockers (existing tasks only) |
| `enter` · `alt+enter` · `shift+enter` | newline in description |
| `ctrl+s` | save |
| `esc` | cancel |

### Blocker picker (from task view via `b`, or from the form via `ctrl+b`)

| Key | Action |
|---|---|
| `↑ ↓` · `j k` | move (auto-scrolls) |
| `pgup` · `pgdn` · `ctrl+u` · `ctrl+d` | scroll by half page |
| `g` · `G` | first / last candidate |
| `space` | toggle blocker |
| `ctrl+s` | save |
| `esc` | cancel |

### Entity view (Settings › entity → `enter`)

| Key | Action |
|---|---|
| `↑ ↓` · `j k` | scroll body |
| `pgup` · `pgdn` · `ctrl+u` · `ctrl+d` | scroll by half page |
| `g` · `G` | jump to top / bottom |
| `e` | edit (opens `$EDITOR`) |
| `M` | toggle markdown render of the entity body (raw ⇄ rendered; default rendered) |
| `d` `d` | arm delete, then confirm |
| `p` | skill picker (persona only) |
| `esc` | back, or cancel a pending delete |

### Skill picker (from a persona via `p`)

| Key | Action |
|---|---|
| `↑ ↓` · `j k` | move (auto-scrolls) |
| `pgup` · `pgdn` · `ctrl+u` · `ctrl+d` | scroll by half page |
| `g` · `G` | first / last row |
| `space` | toggle |
| `enter` on `+ create new` | scaffold new skill |
| `ctrl+s` | save |
| `esc` | cancel |

### Theme / config / template-default pickers

| Key | Action |
|---|---|
| `↑ ↓` · `j k` | move (auto-scrolls) |
| `pgup` · `pgdn` · `ctrl+u` · `ctrl+d` | scroll by half page |
| `enter` | apply (theme: hot-reload; config: restart required; default: clears prior owner) |
| `esc` | cancel |

## File-backed editing — the `$EDITOR` shellout

For skills, laws, personas, and templates, "new" and "edit" actions in **Settings** shell out to the resolved editor (`$EDITOR` → `$VISUAL` → `nano`, in that order; `internal/app/editor.go:ResolveEditor`). When the editor exits successfully, the bundle is re-imported through `app.BundleEditor.Apply` so the SQLite read-model reflects the on-disk change.

Two consequences worth knowing:

- The TUI is paused while the editor runs. After save, the entity view re-renders with the new content.
- If the YAML/frontmatter is invalid, the re-import fails and the error is surfaced as a coded error in the entity view header — fix the file and re-open.

## Default sort, filter, and limits

Per-view defaults come from `config.views` in the active profile yaml (`Settings.EffectiveViews()`). The TUI seeds itself from these on startup. Allowed values and canonical defaults are documented in `.docs/configuration-guide.md` §"`config.views`".

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
- `m.sliceScrollRows(rows, scroll, viewport)` — legacy public API for fixed-height callers. Now a thin wrapper that builds heights of 1s and delegates to `renderScrollWindowSplit`.
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

The TUI loads its theme from `<config-root>/themes/<active>.yaml` with `themes/custom/<active>.yaml` taking precedence (`internal/cli/tui.go:loadActiveTheme`). The active theme key is `config.theme.active`. See `.docs/theming-guide.md` for color tokens and authoring.

## Markdown rendering

Body fields shown in the read-only detail panels (task description, comment body inside the dedicated comment view, and the entity body for laws / personas / skills / templates) render as styled markdown by default. The renderer (`internal/tui/markdown.go`) builds an `ansi.StyleConfig` from the active theme tokens (`primary`, `foreground`, `border`, `secondary`) — switching theme rebuilds the renderer and clears its per-(body, width) cache. Code blocks render as plain mono (no chroma syntax highlight) so they stay aligned with the dev-editorial palette.

Press `M` (capital) inside the task view, comment view, or entity view to toggle between raw and rendered for the rest of the session — useful when you need to copy markdown verbatim or debug formatting. The toggle is session-only; it is not persisted to the active profile yaml. The status badge confirms the active mode (`Markdown rendered` / `Markdown raw`). Editing flows (textareas + `$EDITOR`) are unaffected — they always show raw text.
