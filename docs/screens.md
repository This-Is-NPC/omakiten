# The terminal, in the order you meet it

A walk through `okt tui`, from choosing a project to reading and changing its
work. Use the [how-to pages](README.md) for CLI procedures; this page explains
the visible areas, their focus, and the keys that connect them.

```bash
okt tui
okt --project example tui
```

`?` opens help for the current context. The footer names actions available in
that context; a screen's own keys take precedence over top-level navigation.

## 1. Choose the project in Home

When no project resolves, Home lists registered projects as cards: name, slug,
root, pending work, and tags. `0` or `ctrl+h` returns here from project views.

| Key | Action |
| --- | --- |
| `j` / `k`, down / up | Move project selection. |
| `pgup` / `pgdn`, `ctrl+u` / `ctrl+d` | Page the project cards. |
| `g` / `G` | First / last project. |
| `enter` | Open the selected project's board. |
| `e` | Open its Project view. |
| `n` | Show project creation guidance. |
| `d` | Arm project deletion and follow its confirmation. |
| `q`, `ctrl+c` | Quit. |

Cards and their borders fit inside the column frame. The column owns scrolling,
so the selected project remains reachable in a compact terminal. Confirmed
deletion writes a verified database recovery image before removing the project;
the resulting status reports its path.

With the installed shell wrapper, leaving the TUI changes the parent shell
to the last selected project root. The bare executable writes the handshake
but cannot change its parent shell itself.

## 2. Read the project

`ctrl+p` opens Project from a project view. Metadata, dashboard, and activity
have their own bordered sections. Titles and outer borders stay fixed while
content and scroll indicators remain inside them, following Task Detail's
framing.

| Key | Action |
| --- | --- |
| `tab` | Cycle metadata, dashboard, and activity focus. |
| `j` / `k`, page keys | Move within the focused section. |
| `enter` | Open the focused comment or referenced task. |
| `f` | Open the full-width Project Form reader. |
| `Shift+K` | Open the file-backed project knowledge catalog. |
| `r` | Refresh the projection. |
| `esc` | Return to the prior screen. |

On compact terminals, the focused section receives the available reading
height when all three cannot fit legibly. Wider terminals place metadata and
dashboard beside activity. The full-width reader toggles raw/rendered Markdown
with `M`; `f` or `esc` returns to Project.

## 3. Work from the board

The Tasks area exposes board, table, graph, and plans. These are presentations
of the same operational work: changing views does not move a task or change
its assignee.

Use selection and motion keys to highlight a task, then `enter` for Task
Detail. The footer and `?` show creation, editing, transition, and filtering
keys for the active view. Workflow permissions and guard failures apply here
as they do through the CLI.

The graph shows task dependencies. Plans group tasks by ordered waves, with
a separate plan-network presentation. A wave is an ordering rule, not another
task bucket.

Project Knowledge reads Markdown, OpenAPI, and declared CLI inventories from
the registered root. Its default view starts with CLI, API, and documentation;
select an interface to see its immediate commands, schemas, and linked guides.
Use `enter` to follow a relation, `esc` to return to its parent, `l` for the
flat resource list, `g` for the focused graph, and `r` to reread source files.
See [the knowledge guide](how-to-browse-project-knowledge.md)
for source and cross-project rules.

## 4. Read Task Detail

Task Detail keeps task fields, children, and activity in independently focused
sections. Move between sections with the displayed focus keys, and scroll
inside the selected section. Comments and system events retain their identity
when activity refreshes.

The full-width form reader is for long Markdown. Comment details can offer
read and edit modes when the resolved workflow permissions allow them. An
unsuccessful save keeps the edit buffer and reports the failure.

Task actions are semantic operations: completing work requests a workflow
move, and destructive actions require their configured confirmation and
recovery behavior. A visible action does not bypass a guard.

## 5. Search and navigate

`ctrl+k` opens the palette. Its command side handles configured navigation
and operation verbs; its Search side searches operational records. Selecting
a result opens the appropriate task, comment, or other detail route.

The top-level areas are Tasks, Stats, and Settings. `1`, `2`, and `3` select
them; `tab` and `shift+tab` cycle where the active screen does not own those
keys. `,` and `/` cycle subviews where applicable. `ctrl+o` returns through
navigation history.

## 6. Read activity and metrics

Stats presents project and agent signals. Logs shows the event history,
including task changes, comments, guards, hooks, and CLI activity. Filters
and category colors help distinguish business events from tool calls.

Agent metrics rely on the recorded model and session identity. TUI interaction
is recorded as human activity, rather than attributed to a coding model.

## 7. Change policy in Settings and Studio

Settings exposes workflow and file-backed assets: laws, personas, skills,
templates, and tags. Studio provides supported structured edits with preview
and apply. A candidate package is validated before it becomes the selected
snapshot.

For Markdown assets, editing can open the resolved external editor. Editor
selection follows `$EDITOR`, then `$VISUAL`, then `nano`. Successful edits go
through the package editor and runtime reload; a reload failure remains visible.

Use [workflow customization](how-to-customize-a-workflow.md) to understand the
owning modules and [appearance and language](how-to-change-appearance-and-language.md)
to distinguish application preferences from preset assets.

## While the board changes

The host refreshes projections and watches relevant configuration sources.
Each project keeps its own accepted runtime. Stale asynchronous replies cannot
overwrite a screen that has changed generation or closed.

Reading overlays, editor buffers, and confirmation states retain their own
interaction state. A failed reload reports the error while preserving the
accepted configuration. Component cursors and viewports own their scroll state;
resizing changes the available layout rather than creating another board.
