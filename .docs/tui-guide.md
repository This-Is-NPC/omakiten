# TUI Guide

`okt tui` opens the Bubble Tea terminal UI (`internal/cli/tui.go` → `internal/tui/model.go`). Five top-level views, several modal sub-screens, a multi-project Home, and a contextual help overlay.

## Home (multi-project picker)

When `okt tui` is launched **outside** a registered project (no `--project` / `--project-id`, and the current working directory does not match any registered `root_path`), the TUI opens on the Home Screen. It lists every project in the local SQLite database as a card — name, slug, root path, pending task count, and the project's tags as filled-pill badges.

Home is **outside** the `tab` rotation. Tab/digit keys never land on Home; the only ways in are:

- starting `okt tui` without a resolvable project, or
- pressing `ctrl+h` from any per-project view.

While Home is the active view the per-view tab bar is hidden — Home reads as a chromeless surface.

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

## Views

Cycle with `tab` / `shift+tab`, or jump directly with the digit keys.

| Digit | View | Renderer |
|---|---|---|
| `1` | **Board** — kanban columns, one per workflow bucket | `internal/tui/render_board.go` |
| `2` | **Table** — flat task list with sort/filter | `internal/tui/render_table.go` |
| `3` | **Graph** — dependency DAG | `internal/tui/render_graph.go` |
| `4` | **Config** — entity browser (skills, laws, personas, templates) | `internal/tui/render_config.go`, `entity_screen.go` |
| `5` | **Logs** — operations and per-task activity feeds | `internal/tui/render_logs.go`, `render_activity.go` |

The canonical names live in `internal/tui/state.go:viewNames` (`BOARD`, `TABLE`, `GRAPH`, `CONFIG`, `LOGS`).

## Help overlay

Press `?` from any view to open the keybindings overlay. By default it shows only the bindings relevant to the current context (current view + any open sub-screen). Press `a` to expand to **all contexts** at once — useful for hunting a binding you forgot. The selection logic lives in `internal/tui/render_help.go:currentHelpTitles`.

## Keybindings

The full keymap below is the source of truth in `internal/tui/render_help.go`. Anywhere `j/k` works, `↑/↓` does too.

### Global (always available)

| Key | Action |
|---|---|
| `?` | open / close help overlay |
| `a` | toggle help between current context and all contexts |
| `q` · `ctrl+c` | quit |
| `tab` · `shift+tab` | cycle views forward / backward |
| `1` · `2` · `3` · `4` · `5` | jump to view |
| `ctrl+h` | back to multi-project Home |
| `r` | refresh from store |

### Board (view 1)

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

### Task list (view 2)

| Key | Action |
|---|---|
| `↑ ↓` · `j k` | select task (auto-scrolls) |
| `pgup` · `pgdn` · `ctrl+u` · `ctrl+d` | scroll by half page |
| `g` · `G` | first / last task |
| `enter` | open task |
| `n` | new task |
| `e` | edit task |
| `m` | move by bucket key |

### Graph (view 3)

| Key | Action |
|---|---|
| `← →` | switch view |
| `↑ ↓` · `j k` | move cursor |
| `enter` | open task |
| `pgup` · `pgdn` · `ctrl+u` · `ctrl+d` | scroll by half page |
| `g` · `G` | jump to top / bottom |

### Config (view 4)

| Key | Action |
|---|---|
| `← →` | switch entity kind (skills · laws · personas · templates) |
| `↑ ↓` | select entity |
| `enter` | open detail |
| `n` | new entity (creates the file and shells out to `$EDITOR`) |
| `e` | edit in `$EDITOR` |
| `d` `d` | arm delete, then confirm (two-press fuse) |
| `p` | open the skill picker (when viewing a persona) |

### Logs (view 5)

| Key | Action |
|---|---|
| `← →` | switch sub-view (operations ↔ task activity) |
| `↑ ↓` · `j k` | select row (auto-scrolls) |
| `pgup` · `pgdn` · `ctrl+u` · `ctrl+d` | scroll by half page |
| `g` · `G` | first / last row |
| `r` | refresh |

## Modal sub-screens

These open on top of a view and intercept all input until dismissed. The contextual help overlay (`?`) automatically narrows to whichever sub-screen is open.

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
| `esc` | back to board |

### Comment view (after `enter` on a comment)

| Key | Action |
|---|---|
| `↑ ↓` · `j k` | scroll body |
| `pgup` · `pgdn` · `ctrl+u` · `ctrl+d` | scroll by half page |
| `g` · `G` | jump to top / bottom |
| `esc` | back to task view |

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

### Entity view (Config → `enter`)

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

## File-backed editing — the `$EDITOR` shellout

For skills, laws, personas, and templates, "new" and "edit" actions in the **Config** view shell out to the resolved editor (`$EDITOR` → `$VISUAL` → `nano`, in that order; `internal/app/editor.go:ResolveEditor`). When the editor exits successfully, the bundle is re-imported through `app.BundleEditor.Apply` so the SQLite read-model reflects the on-disk change.

Two consequences worth knowing:

- The TUI is paused while the editor runs. After save, the entity view re-renders with the new content.
- If the YAML/frontmatter is invalid, the re-import fails and the error is surfaced as a coded error in the entity view header — fix the file and re-open.

## Default sort, filter, and limits

Per-view defaults come from `config.views` in `omakiten.yaml` (`Settings.EffectiveViews()`). The TUI seeds itself from these on startup. Allowed values and canonical defaults are documented in `.docs/configuration-guide.md` §"`config.views`".

## Live refresh

Views with task state (`Board`, `Table`, `Graph`) emit a refresh tick about once a second while idle (`internal/tui/state.go:refreshTickMsg`). Editing or having a modal open suppresses the tick — the realtime refresh logic lives in `Model.shouldRealtimeRefresh`.

`r` forces an immediate refresh on `Board` and `Logs`.

## Theming

The TUI loads its theme from `<config-root>/themes/<active>.yaml` with `themes/custom/<active>.yaml` taking precedence (`internal/cli/tui.go:loadActiveTheme`). The active theme key is `config.theme.active`. See `.docs/theming-guide.md` for color tokens and authoring.
