# Buddies

Buddies are configurable ASCII mascots that pop over the Omakiten TUI
when a domain event matches a `buddy.show` hook. They turn quiet
notifications (a guard violation, an agent comment) into a visible
beat — the buddy types its message into a speech balloon, plays a
tiny animation, and dismisses on key, timeout, or next domain
transition.

This guide covers the buddy YAML schema, color references, and how
buddies plug into the hooks engine. For the dispatch lifecycle and
the `buddy.show` arg contract, see [`hooks.md`](hooks.md).

## Lifecycle in three lines

1. A loaded buddy lives at `buddies/<name>.yaml` (default) or
   `buddies/custom/<name>.yaml` (override). Drop a file in either and
   it loads at startup — no entry in `omakiten.yaml`.
2. Pick which one is active via `config.tui.buddy.active`.
3. Wire it to events with one or more `do: buddy.show` hooks; the
   action sends a `ShowMsg` into the running tea program.

## File layout

```
config/                 # active config
buddies/
  kitten.yaml           # default — refreshed by RefreshDefaultFiles
  owl.yaml              # default
  custom/
    capybara.yaml       # user-authored, never overwritten
themes/
omakiten.yaml
```

`buddies/custom/<slug>.yaml` overrides any default that shares a
`name:`. Two files at the same scope (both default OR both custom)
with the same name fail at `LoadBundle`.

## Buddy YAML schema

```yaml
name: kitten                  # required; canonical id used in config.tui.buddy.active
description: short blurb      # required
size:                         # required; inner content rect (border sits OUTSIDE)
  width: 22                   # > 0
  height: 8                   # > 0
background: transparent       # required; transparent | $theme.<key> | #rrggbb
frame_interval_ms: 600        # required; > 0 — animation cadence; can be overridden per hook
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
animations:                   # required; at least one animation, each with frames 0..N-1
  idle:
    - frame: 0
      value: |2
        /\___/\
       ( o   o )
    - frame: 1
      value: |2
        /\___/\
       ( -   - )
  deny:
    - frame: 0
      value: |2
        /\___/\
       ( x   x )
bubble:
  tail_side: bottom           # required; bottom | top | left | right
```

The validator rejects:

- any required field missing or zero,
- unknown `style` values,
- `style: custom` with a partial `custom_border`,
- `border.visible: true` with `width <= 0` or empty `color`,
- `animations` empty, an animation with zero frames, frames whose
  indices are not `0..N-1` (gaps OR duplicates), an empty `value`,
- `bubble.tail_side` outside the closed set,
- color values outside the resolver grammar (see below).

Error messages cite the buddy name + source path so the user can
pinpoint the offending file even when several buddies are loaded.

## Colors

Three forms are accepted anywhere a color is expected
(`background`, `border.color`, `border.background`):

| Form              | Resolves to                                  |
| ----------------- | -------------------------------------------- |
| `transparent`     | `lipgloss.NoColor{}` — terminal default.     |
| `$theme.<key>`    | The active theme's `colors[<key>]` hex.      |
| `#rrggbb`         | Literal hex, six digits.                     |

Theme references resolve at every `View()` call against the live
theme, so swapping themes via the in-app picker repaints the buddy
on the next frame without rebuilding it. Use the literal hex form
only when the buddy needs a fixed brand color regardless of the
user's theme.

The resolver rejects:

- empty values;
- short hex (`#fff`);
- `$theme.<key>` where the key does not exist on the active theme;
- anything else.

## Activating a buddy

```yaml
config:
  tui:
    buddy:
      active: kitten
```

The validator hard-rejects empty `active` when at least one entry under
`config.hooks` does `do: buddy.show`. With no buddy hooks the field is
optional — the catalogue still loads; nothing is wired.

## Wiring a buddy to events

Every animation timing, position, dismiss strategy, and message field
is declared on the hook (not the buddy YAML), so the same buddy can
behave differently per event. The full arg schema lives in
[`hooks.md` § `buddy.show`](hooks.md#buddyshow).

```yaml
config:
  hooks:
    - on: guard.violated
      when: { operation: task.delete }
      do: buddy.show
      args:
        animation: deny
        position: top-right
        typing_ms_per_char: 25
        dismiss: { mode: key, keys: [esc] }
        message_field: hint
```

`message_field` is a top-level key of `domain.Event.Payload`. If the
key is absent or empty, the bubble falls back to `domain.Event.Body`.
A bubble with no resolved text aborts the show — the buddy never
appears with an empty balloon.

`typing_ms_per_char: 0` is the “instant” shortcut: the buddy reaches
the Settled state on the first tick, which in turn starts the
`timeout` clock immediately.

## Default buddies

The kit ships two:

- **kitten** — playful default, paints in `$theme.primary`, idle
  animation blinks the eyes, deny narrows them.
- **owl** — wise/contemplative contrast, paints in
  `$theme.secondary`, slower frame cadence (900 ms), same idle/deny
  animation pair so users can swap in either via
  `config.tui.buddy.active` without rewiring hooks.

Both are reasonable starting points for a custom mascot — copy the
YAML into `buddies/custom/<your-buddy>.yaml`, edit `name:` so it
does not collide with the default, and tweak the frames/colors.

## Behaviour notes

- A buddy is exclusive while alive: it consumes scroll keys for its
  own bubble (`j`/`k`, page navigation, `g`/`G`, `home`/`end`) and
  swallows non-dismiss keys so the app underneath stays inert.
- A new `buddy.show` event that arrives while the current buddy is
  still typing is dropped; once the buddy is Settled, the new
  payload replaces it.
- Theme switches repaint the buddy on the next View — colors are
  resolved at render time, not at construction.
- The action lives in `internal/tui/components/buddy/`; the runtime
  registers it from CLI/MCP/TUI composition roots so hook validation
  works the same in every entry point. Outside the TUI it is a
  silent no-op.
