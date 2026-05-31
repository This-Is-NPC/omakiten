# Notifications

Notifications are configurable popup cards that paint over the
Omakiten TUI when a domain event matches a hook. Each notification is
a **self-contained recipe** — the YAML file declares everything the
renderer needs (card geometry, optional ASCII animation, position,
dismiss strategy, message text). The active profile yaml only links events to
notification slugs.

The mental model: think of each notification file as a reusable visual
recipe. Different copy per event can live on the hook entry; different
look, placement, timing, or animation means a different notification
recipe. The visual config may duplicate across files — that's the price
for modularity.

## Lifecycle in three lines

1. A notification lives at `notifications/<slug>.yaml` (default) or
   `notifications/custom/<slug>.yaml` (override). Drop a file in
   either and it loads at startup — no entry in the active profile yaml.
2. The active profile yaml under `config.hooks` links event matches to a
   notification slug via the `notification: <slug>` field.
3. The hooks engine runs on the event bus; when a hook matches, the
   action looks up the notification by slug and emits a card.

Notifications are **opt-in** and **animation-optional**: a card
without an `animation:` block renders as a plain bubble, no ASCII
mascot. Authors that want a quiet text-only alert can omit the
animation block entirely.

## File layout

```text
config/
  <active>.yaml            # active profile yaml (e.g. omakase.yaml)
  custom/
    <profile>.yaml         # user-authored profile overrides
notifications/
  <slug>.yaml              # default — refreshed on every install sync
  custom/
    <slug>.yaml            # user-authored, never overwritten
themes/
  <key>.yaml               # default themes
```

`notifications/custom/<slug>.yaml` overrides any default that shares
the same `name:`. Two files at the same scope (both default OR both
custom) with the same name fail at `LoadBundle`. Custom files that
fail validation are SKIPPED with a warning printed to stderr — the
runtime stays usable; only that one card is dropped.

## Notification YAML schema

The embedded defaults under `defaults/notifications/*.yaml` are useful
starting points when authoring a new notification. Copy one into
`notifications/custom/<your-slug>.yaml`, change `name:` to match the
slug, and wire it from `config.hooks`.

Notification YAML has no code-side visual defaults: every render or behaviour
knob is either explicitly declared in the file or rejected by validation. To
opt out of a visual feature, write that choice in YAML (for example
`background: transparent`, `footer_visible: false`, or padding sides set to
`0`).

```yaml
# Identity
name: task-guard                   # required; should match the filename slug
description: short blurb           # required

# Geometry
size:
  width: 28                        # required; > 0 — outer card width in cells (border + padding + content + border)
  height: 12                       # required; > 0 — pinned outer height when auto_height=false; bubble scroll viewport cap

# Behaviour flags (required; declare the value you want)
auto_height: true                  # true → card flows to body height. false → outer height pinned to size.height
padding_inside: false              # false → body top-anchored. true (auto_height=false) → body vertically centered
footer_visible: false              # false → no footer. true → key hints on a reserved bottom row
footer_position: left              # required when footer_visible=true; left | center | right alignment for the footer row(s)

# Background + chrome
background: $theme.highlight       # required; transparent | $theme.<key> | #rrggbb
style: rounded                     # required; rounded | square | double | thick | hidden | custom
border:
  visible: true                    # required
  width: 1                         # required when visible — > 0
  color: $theme.primary            # required when visible
  background: transparent          # optional; same grammar as color
custom_border:                     # required ONLY when style: custom
  top: "─"
  bottom: "─"
  left: "│"
  right: "│"
  top_left: "╭"
  top_right: "╮"
  bottom_left: "╰"
  bottom_right: "╯"

# Inner padding (cells eaten from the frame BEFORE the body renders).
# Footer + scroll hint sit OUTSIDE this band — padding.bottom adds
# rows BETWEEN the body and the footer, not after it.
padding:
  top: 1                           # required; set 0 for no padding
  right: 2                         # required
  bottom: 1                        # required
  left: 2                          # required

# Placement
position: center                   # required; one of nine fixed anchors (see below)

# Bubble + tail
bubble:
  tail_side: bottom                # required when animation is set; bottom | top | left | right

# Animation (optional — omit the block for a plain bubble notification)
frame_interval_ms: 600             # required ONLY when `animation:` is set; > 0
animation:
  - frame: 0                       # frames are indexed 0..N-1, gap-free
    value: |2
        /\___/\
       ( o   o )
  - frame: 1
    value: |2
        /\___/\
       ( -   - )

# Bubble text
typing_ms_per_char: 0              # required; >= 0; 0 = show full bubble immediately
message: "Move blocked here."      # one of message OR message_field per layer; mutually exclusive
message_field: hint                # alternate to message — top-level key to read from event.payload

# Dismiss
dismiss:
  mode: key                        # required; key | timeout | next_status
  keys: [esc, q, enter, " "]       # required when mode=key; optional with timeout for manual close
  after_ms: 8000                   # required when mode=timeout

# Actions (optional — turns the notification into an interactive prompt)
actions:
  - key: m                         # single keystroke that fires this action
    id: migrate                    # stable identifier surfaced to the audit log
    label: Migrate                 # rendered in the footer next to the keystroke
    command: ["workflow", "orphans", "--confirm"]  # cobra args run in-process when the key is pressed; omit/empty for a labeled dismiss
  - key: s
    id: skip
    label: Skip                    # no command → behaves like a labeled dismiss
```

## Action buttons

When `actions:` is present, the notification turns into a prompt with one
button per entry. The TUI consumes the action's `key` ahead of any `dismiss.keys`
entry, emits a `confirmation.granted` event tied to the active project, and
dispatches `command` in-process through the same cobra root the CLI uses.
Empty/absent `command` means the action only labels a dismiss path — useful
for "Skip" / "Cancel" buttons.

Templating: each `command` element runs through `text/template` (Go stdlib)
with the triggering event's payload exposed as `{{ .Payload.* }}`. Missing
keys raise a loud error so a typo in YAML fails fast instead of silently
shipping the wrong arguments. Example: `["workflow", "orphans", "--id={{ .Payload.id }}", "--confirm"]`.

The validator rejects:

- empty `key`, `id`, or `label`,
- a `key` that duplicates another action in the same notification,
- an `id` that duplicates another action in the same notification,
- a `key` that also appears in `dismiss.keys` (actions take priority, but
  the overlap is ambiguous — clear one side),
- a `command` whose first element is `tui` or `mcp` (those surfaces need
  their own terminal and cannot run nested under a hook).

Audit trail: every successful action key emits `confirmation.granted` with
payload `{notification_slug, action_id, command}` before the command runs.
The event's `author_type` flows from the TUI context (`human`) so reviewers
can trace human-approved automation in the activity log.

The validator rejects:

- any required field missing or invalid,
- unknown `style` / `position` / `dismiss.mode` / `bubble.tail_side`,
- unknown `footer_position`,
- `style: custom` with a partial `custom_border`,
- `border.visible: true` with `width <= 0` or empty `color`,
- frame indices not `0..N-1` (gaps OR duplicates), empty frame
  `value`, `frame_interval_ms <= 0` when an animation is set,
- `dismiss.mode=key` with no `keys`; any empty `dismiss.keys` entry;
  `dismiss.mode=timeout` with `after_ms <= 0`,
- both `message` and `message_field` set on the same layer,
- missing `padding.*` or `padding.*` < 0.

The combined-presence rule (at least one of `message` /
`message_field` between the notification YAML and the hook entry) is
enforced by the hooks validator at LoadBundle. Errors cite the
notification name + source path so the offending file is easy to
find.

## Positions

Nine fixed anchors. The card overlay is computed against the FULL
terminal grid, so `center` lands in the actual middle of the screen
regardless of which TUI surface is open.

`top-left`, `top-center`, `top-right`,
`middle-left`, `center`, `middle-right`,
`bottom-left`, `bottom-center`, `bottom-right`

## Layout: tail_side drives orientation

| Tail side | Layout                                                        |
| --------- | ------------------------------------------------------------- |
| `bottom`  | Vertical — bubble on top, frame on bottom, tail `\V/`.        |
| `top`     | Vertical — frame on top, bubble on bottom, tail `/\`.         |
| `right`   | Horizontal — bubble on left, frame on right, tail `>` column. |
| `left`    | Horizontal — frame on left, bubble on right, tail `<` column. |

For vertical layouts the frame is horizontally centered so the
centered tail glyph visually points at the animation. When the
notification has no animation, the tail and frame are skipped and the
bubble fills the body region alone.

## Sizing modes

| `auto_height` | Behaviour                                                                                                         |
| ------------- | ----------------------------------------------------------------------------------------------------------------- |
| `true`        | Card outer height tracks the rendered body. `size.height` only caps the bubble's scroll viewport.              |
| `false`       | Card outer height is pinned to `size.height`. The bubble scrolls inside the body region; tail + frame stay fixed. |

When `auto_height=false`:

- `padding_inside=true` vertically centers the body inside the body
  region, splitting blank rows top + bottom. Useful for square cards.
- A scroll hint (`▲ N above · ▼ N below`) auto-renders on a reserved
  row between the body and the footer when bubble content overflows
  the visible region. The row is reclaimed when nothing is hidden.

## Footer

`footer_visible: true` reserves the bottom row of the card for key hints. The
footer can show a dismiss-key hint (`esc/q/enter/space close`) and, when a hook
supplies a detail page, the `tab details` hint. `dismiss.mode=timeout` may also
declare `keys`, giving the card both auto-dismiss and manual close. With
`next_status`, close keys are not shown. The footer is rendered at the frame
width — it ignores horizontal `padding` so the band sits flush edge-to-edge.

`footer_position` controls how each footer row aligns inside that full-width
band: `left`, `center`, or `right`. This affects only the footer;
card `padding` still applies to the bubble/body region above it.

## Colors

Three forms accepted anywhere a color is expected
(`background`, `border.color`, `border.background`):

| Form              | Resolves to                              |
| ----------------- | ---------------------------------------- |
| `transparent`     | `lipgloss.NoColor{}` — terminal default. |
| `$theme.<key>`    | Active theme's `colors[<key>]` hex.      |
| `#rrggbb`         | Literal hex, six digits.                 |

Theme references resolve at every `View()` call so the in-app theme
picker repaints the card on the next frame.

## Wiring events to notifications

```yaml
config:
  hooks:
    - on: guard.violated
      notification: task-guard                     # → notifications/task-guard.yaml
    - on: comment
      when: { author_type: agent }
      notification: agent-note
      message: "Agent dropped a comment"           # optional hook-level fallback
      detail_message_field: hint                    # optional tab detail page
```

Each hook entry uses **either** `notification: <slug>` or
`do: <action>` + `args:` (exec/noop dispatch). Mixing both shapes in
the same entry fails validation. Unknown slugs fail at `LoadBundle`.

### Per-kit catalog resolution

When a sub-task kit is wired ([`subtask-kit.md`](subtask-kit.md)), each hook entry's resolved kit identity is threaded through dispatch so a slug declared in the sub-kit's `config.hooks` resolves through the **sub-kit's** notification catalog at runtime — not the root's. The action looks up the slug in `NotificationsByKit[<resolved_kit>][<slug>]` first and falls back to the root `Notifications` map only when the per-kit catalog has no entry for that slug. Root-kit hooks dispatch identically through the root catalog.

The practical effect: a sub-kit can ship a `notifications/subtask-orphaned-warning.yaml` and reference it from its own `config.hooks` without the root kit needing to know the slug exists. Collisions (a slug declared in both kits' catalogs) resolve per-kit: each kit sees its own chrome / copy.

### Message resolution

Either layer (notification YAML or hook entry) may declare `message`
(literal) or `message_field` (payload key). The validator rejects
the combo when neither layer supplies any source. **Notification YAML
wins on tie-break.** Resolution order:

1. `notification.message` (literal in the notification YAML)
2. `event.payload[notification.message_field]`
3. `hook.message` (literal in the active profile yaml under `config.hooks`)
4. `event.payload[hook.message_field]`
5. `event.body` (last-resort fallback)

`message` and `message_field` are mutually exclusive within each
layer; declare one or the other, never both.

### Detail page

Notification hooks may declare `detail_message` (literal) or
`detail_message_field` (payload key). These fields do not replace the primary
bubble text. When they resolve to non-empty text, the TUI footer advertises
`tab details`, and pressing `tab` toggles the bubble between the short message
and the detail page. The detail page uses the same scroll keys as long primary
messages.

This is useful for defaults that keep the first page playful while still
letting the user inspect the complete guard hint:

```yaml
config:
  hooks:
    - on: guard.violated
      when: { operation: task.delete }
      notification: task-guard
      message: "Trying to burn the quest log? Adorable."
      detail_message_field: hint
```

## Default notifications

The default kit ships reusable notification recipes for common workflow
events. Event-specific copy lives in `config.hooks`, so one visual recipe can
serve many operations while each hook keeps its own message and optional detail
text.

Default recipes use timeout dismissal with manual close keys: routine notices
settle and disappear automatically, while guard/destructive notices can stay a
little longer and expose `tab details` when the hook provides a detail message.
Copy any default into `notifications/custom/<your-slug>.yaml`, change `name:` to
match the new filename, tweak frames, and reference the slug from a new
`config.hooks` entry in the active profile yaml.

## Guard hints and i18n

Guard hint strings reaching a notification payload (`event.payload.hint`
on `guard.violated`, surfaced via `message_field: hint` or
`detail_message_field: hint`) may embed `${{intl:KEY}}` tokens so preset
YAMLs keep hint copy in the locale catalog instead of inlining a single
language. `resolveHint` in `internal/app/guards/evaluator.go:212`
expands the tokens against the active i18n catalog via
`Snapshot.ResolveGuardHint` before the violation event is emitted, so
the notification always renders the resolved string.

Fallback chain when a key is absent: active locale → baseline locale →
**literal key text**. `Catalog.Get` returns the key verbatim and logs
at debug level (`catalog: key missing from active and baseline;
returning key literal`) — the card never displays an empty bubble, but
it may show the bare key when a translation is missing. Malformed
tokens (`${{intl:`, `${{intl:}}`) are left verbatim with a debug-level
log line; only well-formed `${{intl:KEY}}` is substituted.

## Update when

- A new notification slug ships under `defaults/notifications/`.
- The card schema gains a field (`internal/notifications/loader.go`).
- The `${{intl:KEY}}` interpolation contract changes.
- The exclusivity / scroll-routing behaviour shifts in `internal/tui/notifications/`.

## See also

- [`mcp.md`](../mcp.md) — agent-facing tool surface; useful when wiring
  a notification body to a query result instead of the raw triggering
  payload.
- [`hooks.md`](hooks.md) — hook entries that dispatch notifications.
- [`system.md § config.hooks`](system.md#confighooks) — hook subscription
  shape that pulls notification cards in.

## Behaviour notes

- Notifications are **opt-in**: an empty `config.hooks` block ships
  no cards. The system has no global "active mascot" — each event
  declares its own.
- A notification is exclusive while alive: it consumes scroll keys
  for its bubble (`j`/`k`, page nav, `g`/`G`, `home`/`end`) — only
  the bubble scrolls, the animation stays put — and swallows
  non-dismiss keys so the app underneath stays inert.
- A new notification event arriving while the current card is still
  typing is dropped; once Settled, the new payload replaces it.
- The notification action is registered from CLI / MCP / TUI
  composition roots so hook validation works the same in every entry point.
  Outside the TUI it is a silent no-op.
