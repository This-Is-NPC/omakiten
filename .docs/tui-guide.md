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
| `enter` | open task |
| `n` | new task |
| `e` | edit task |
| `c` | add comment |
| `m` | move task between lanes |

### Tasks › Table

| Key | Action |
|---|---|
| `↑ ↓` · `j k` | select task (auto-scrolls) |
| `pgup` · `pgdn` · `ctrl+u` · `ctrl+d` | scroll by half page |
| `g` · `G` | first / last task |
| `enter` | open task |
| `n` | new task |
| `e` | edit task |
| `m` | move by bucket key |

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

- **Runtime**: `okt version`, `omakiten.yaml` path, SQLite path
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

### Task view (after `enter` on a task card)

| Key | Action |
|---|---|
| `tab` · `shift+tab` | switch focus (form ⇄ activity column) |
| `↑ ↓` · `j k` | scroll description (form focus) · navigate cards (activity focus) |
| `J` · `K` | navigate activity cards regardless of focus |
| `enter` | open the focused comment in the comment-detail view |
| `pgup` · `pgdn` · `ctrl+u` · `ctrl+d` | scroll by half page |
| `g` · `G` | jump to top / bottom |
| `e` | edit |
| `b` | edit blockers (opens the blocker picker) |
| `c` | add comment |
| `m` | move |
| `esc` | back |

### Comment view (after `enter` on a comment)

| Key | Action |
|---|---|
| `↑ ↓` · `j k` | scroll body |
| `pgup` · `pgdn` · `ctrl+u` · `ctrl+d` | scroll by half page |
| `g` · `G` | jump to top / bottom |
| `esc` | back |

### Comment input (after `c`)

| Key | Action |
|---|---|
| `enter` | save comment |
| `alt+enter` · `shift+enter` | insert newline |
| `esc` | cancel |

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

Per-view defaults come from `config.views` in `omakiten.yaml` (`Settings.EffectiveViews()`). The TUI seeds itself from these on startup. Allowed values and canonical defaults are documented in `.docs/configuration-guide.md` §"`config.views`".

## Live refresh

Per-project zones emit a refresh tick about once a second while idle (`internal/tui/state.go:refreshTickMsg`). Editing, an open modal, or sitting on Home suppresses the tick — see `Model.shouldRealtimeRefresh`. The tick wraps its context with `activity.WithoutTracking` so its app-service calls do not pollute the activity log; `r` and refreshes after a view change still track.

## Scroll abstraction

Panels that page through long lists (Tasks › Table, Stats › Logs, Tasks › Graph, every picker) share two helpers:

- `sliceScrollRows(dataRows, scroll, viewport)` — given the panel's full row budget, produces the visible slice plus the `▲ above` / `▼ below` hint rows when content overflows.
- `scrollDataRows(viewport)` — the canonical adapter for cursor-tracking helpers (`followCursor`, `picker.Model.Update`, bespoke syncs). Subtracts the worst-case 2 rows that `sliceScrollRows` reserves for hints, so the cursor never lands in the reserved band.

Contract: pass the raw `*ViewportRows()` budget to `sliceScrollRows`; pass `scrollDataRows(*ViewportRows())` to anything that decides scroll from cursor. Documented on the helper at `internal/tui/scroll.go`.

## Theming

The TUI loads its theme from `<config-root>/themes/<active>.yaml` with `themes/custom/<active>.yaml` taking precedence (`internal/cli/tui.go:loadActiveTheme`). The active theme key is `config.theme.active`. See `.docs/theming-guide.md` for color tokens and authoring.
