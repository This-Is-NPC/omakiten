# Notifications

Notifications are configurable ASCII notification cards that pop over the
Omakiten TUI when a domain event matches a hook. Each notification is a
**self-contained recipe** — the YAML file declares everything the
renderer needs (visuals, animation, position, dismiss strategy,
message text). `omakiten.yaml` only links events to notification slugs.

The mental model: think of each notification file as a one-line
`task.delete = notification_error_msg_task_delete.yml` mapping. Custom copy
per event? New notification file. Different look per situation? New notification
file. Yes, this duplicates the visual config across files — that's
the price for modularity, and the user explicitly chose it.

## Lifecycle in three lines

1. A notification lives at `notifications/<slug>.yaml` (default) or
   `notifications/custom/<slug>.yaml` (override). Drop a file in either and
   it loads at startup — no entry in `omakiten.yaml`.
2. `omakiten.yaml::config.hooks` links event matches to a notification slug
   via the `notification: <slug>` field.
3. The hooks engine runs on the event bus; when a hook matches, the
   action looks up the notification by slug and emits a notification card.

## File layout

```
config/                   # active config
notifications/
  guard-violation.yaml    # default — refreshed on every install sync
  agent-comment.yaml      # default
  custom/
    error-task-delete.yaml  # user-authored, never overwritten
themes/
omakiten.yaml
```

`notifications/custom/<slug>.yaml` overrides any default that shares a
filename slug. Two files at the same scope (both default OR both
custom) with the same slug fail at `LoadBundle`.

## Notification YAML schema

```yaml
name: guard-violation         # required; canonical id used by hook entries
description: short blurb      # required
size:                         # required; inner content rect (border sits OUTSIDE)
  width: 28                   # > 0 — visible cell width
  height: 12                  # > 0 — bubble scroll viewport row cap (does NOT pad the card)
background: $theme.highlight  # required; transparent | $theme.<key> | #rrggbb
frame_interval_ms: 600        # required; > 0 — ASCII animation cadence
style: rounded                # required; rounded | square | double | thick | hidden | custom
border:
  visible: true               # required
  width: 1                    # required when visible — > 0
  color: $theme.primary       # required when visible
  background: transparent     # optional; same grammar as color
custom_border:                # required ONLY when style: custom; ignored otherwise
  top: "─"
  bottom: "─"
  left: "│"
  right: "│"
  top_left: "╭"
  top_right: "╮"
  bottom_left: "╰"
  bottom_right: "╯"
position: center              # required; one of nine fixed anchors (see below)
typing_ms_per_char: 0         # required; >= 0 — 0 means "show full bubble immediately"
message: "Move blocked here." # required IF message_field unset
message_field: hint           # required IF message unset; mutually exclusive with message
dismiss:
  mode: key                   # required; key | timeout | next_status
  keys: [esc, q, enter]       # required when mode=key
  after_ms: 8000              # required when mode=timeout
animation:
  - frame: 0                  # frames are indexed 0..N-1, gap-free
    value: |2
        /\___/\
       ( o   o )
  - frame: 1
    value: |2
        /\___/\
       ( -   - )
bubble:
  tail_side: bottom           # required; bottom | top | left | right
```

The validator rejects:

- any required field missing or zero,
- unknown `style` / `position` / `bubble.tail_side` / `dismiss.mode`,
- `style: custom` with a partial `custom_border`,
- `border.visible: true` with `width <= 0` or empty `color`,
- empty `animation`, frame indices not `0..N-1` (gaps OR duplicates),
  empty frame `value`,
- `dismiss.mode=key` with no `keys`; `dismiss.mode=timeout` with
  `after_ms <= 0`,
- both `message` and `message_field` set; or neither.

Errors cite the notification name + source path so the offending file is
easy to find when several notifications are loaded.

## Positions

Nine fixed anchors. The card overlay is computed against the FULL
terminal grid, so `center` lands in the actual middle of the screen
regardless of which TUI surface is open.

`top-left`, `top-center`, `top-right`,
`middle-left`, `center`, `middle-right`,
`bottom-left`, `bottom-center`, `bottom-right`

## Layout: tail_side drives orientation

| Tail side | Layout                                                       |
| --------- | ------------------------------------------------------------ |
| `bottom`  | Vertical — bubble on top, frame on bottom, tail `\V/`.       |
| `top`     | Vertical — frame on top, bubble on bottom, tail `/\`.        |
| `right`   | Horizontal — bubble on left, frame on right, tail `>` column. |
| `left`    | Horizontal — frame on left, bubble on right, tail `<` column. |

The frame is always horizontally centered for vertical layouts so
the centered tail glyph visually points at the kitten.

## Colors

Three forms accepted anywhere a color is expected
(`background`, `border.color`, `border.background`):

| Form              | Resolves to                                  |
| ----------------- | -------------------------------------------- |
| `transparent`     | `lipgloss.NoColor{}` — terminal default.     |
| `$theme.<key>`    | The active theme's `colors[<key>]` hex.      |
| `#rrggbb`         | Literal hex, six digits.                     |

Theme references resolve at every `View()` call, so the in-app theme
picker repaints the notification on the next frame.

## Wiring events to notifications

```yaml
config:
  hooks:
    - on: guard.violated
      notification: guard-violation                  # → notifications/guard-violation.yaml
    - on: comment
      when: { author_type: agent }
      notification: agent-comment
      message: "Agent dropped a comment"     # optional hook-level fallback
```

Each hook entry uses **either** `notification: <slug>` (notification card)
or `do: <action>` + `args:` (legacy exec/noop). Mixing both shapes
in the same entry fails validation.

`notification: <slug>` must resolve to a loaded notification file; unknown slugs
fail at `LoadBundle`.

### Message resolution

Either layer (notification YAML or hook entry) may declare `message`
(literal) or `message_field` (payload key). Both layers may set
neither, but the validator rejects the combo if neither layer
supplies any source. **Notification YAML wins on tie-break** — useful
when a single notification fires on many events but the user wants to
override the wording from omakiten.yaml without touching the notification
file. Resolution order:

1. `notification.message` (literal in the notification YAML)
2. `event.payload[notification.message_field]`
3. `hook.message` (literal in `omakiten.yaml::config.hooks`)
4. `event.payload[hook.message_field]`
5. `event.body` (last-resort fallback)

`message` and `message_field` are mutually exclusive within each
layer; declare one or the other, never both.

## Default notifications

The kit ships two:

- **guard-violation** — centered card with border + theme-highlight
  background; rendered when any `guard.violated` event fires.
- **agent-comment** — top-right card that auto-dismisses 8s after
  it settles; fires when an agent posts a comment.

Both are reasonable starting points for custom notifications — copy the
YAML into `notifications/custom/<your-slug>.yaml`, change `name:` to match
the new filename, tweak the message + frames, and reference it from
a new `omakiten.yaml::config.hooks` entry.

## Behaviour notes

- Notifications are **opt-in**: an empty `config.hooks` block ships no
  notifications. The system has no global "active mascot" — each event
  declares its own.
- A notification is exclusive while alive: it consumes scroll keys for its
  bubble (`j`/`k`, page nav, `g`/`G`, `home`/`end`) and swallows
  non-dismiss keys so the app underneath stays inert.
- A new notification event arriving while the current notification is still typing
  is dropped; once the notification is Settled, the new payload replaces it.
- The runtime registers the action from CLI/MCP/TUI composition
  roots so hook validation works the same in every entry point.
  Outside the TUI it is a silent no-op.
