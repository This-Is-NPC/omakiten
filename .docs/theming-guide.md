# Theming Guide

The TUI's appearance is fully driven by a YAML theme file. Themes live in `<config-root>/themes/<key>.yaml`, are loaded by `internal/config/theme_loader.go:LoadTheme`, validated by `internal/config/validator.go:ValidateTheme`, and consumed by `internal/tui/styles.go:newStyles`.

The active theme key is `config.theme.active` in `omakiten.yaml`.

## Resolution order

When the TUI starts, the theme path is resolved in this order (`internal/cli/tui.go:loadActiveTheme`):

1. `<config-root>/themes/custom/<active>.yaml` — user override (preferred when present).
2. `<config-root>/themes/<active>.yaml` — default kit.

`<config-root>` is the directory holding `omakiten.yaml` (typically `~/.config/omakiten/`). The "custom" path lets you tweak a shipped theme — or add your own — without losing changes when defaults are refreshed.

## File schema

```yaml
version: 1
key:  my-theme               # required, non-empty; must match the filename without .yaml
name: My Theme               # required, non-empty; display name
colors:                      # required, non-empty map of <token> → CSS-style hex
  background:  "#121212"
  foreground:  "#E5E2E1"
  primary:     "#39FF14"
  secondary:   "#8FAE9A"
  success:     "#86D27A"
  warning:     "#FFB347"
  error:       "#FF5544"
  border:      "#494543"
  highlight:   "#1A1A19"
  badge_fg:    "#1A1A1A"     # optional
```

Validation rules (`ValidateTheme`):

- `version` must be exactly `1`.
- `key` and `name` must be non-empty.
- `colors` must be non-empty.

There is no schema check on individual color values — anything Lipgloss accepts as a `lipgloss.Color` will work, but in practice every shipped/recommended theme uses 6-digit `#RRGGBB`.

## Color tokens

Eight tokens are actually consumed by the TUI today (`internal/tui/styles.go`). The other tokens (`background`, `highlight`) are conventional in the shipped themes and reserved for future surfaces — they currently have no visible effect.

| Token | Default | Where it shows up |
|---|---|---|
| `foreground` | `#E5E2E1` | Body text in panels, cards, comments, inputs, the entity view, the task form. |
| `border` | `#494543` | All panel/card/column borders, separators, footer/hint text, system-event card text, the empty-state column placeholder. |
| `primary` | `#39FF14` | Title, active nav highlight, selected-card border, marker glyph, focused-input border, accent text in hints, badge accents. |
| `secondary` | `#8FAE9A` | Inactive nav, system-event card text, secondary metadata. |
| `success` | `#86D27A` | Success badges, the green token-budget pill. |
| `warning` | `#FFB347` | Warning badges, the yellow token-budget pill. |
| `error` | `#FF5544` | Error badges, the red token-budget pill. |
| `badge_fg` | `#1A1A1A` | Foreground used **on filled-pill badges** (dark text over a bright pill). Override this when the rest of the palette is dark — it must contrast with `success`/`warning`/`error`. |

`background` and `highlight` are unused by the current renderer. Including them is harmless and keeps your theme forward-compatible if/when they are wired up.

## Authoring a theme

1. Pick a key in kebab-case (e.g. `my-theme`).
2. Create `<config-root>/themes/custom/my-theme.yaml`:
   ```yaml
   version: 1
   key:  my-theme
   name: My Theme
   colors:
     foreground: "#E5E2E1"
     border:     "#494543"
     primary:    "#39FF14"
     secondary:  "#8FAE9A"
     success:    "#86D27A"
     warning:    "#FFB347"
     error:      "#FF5544"
   ```
3. Activate it in `omakiten.yaml`:
   ```yaml
   config:
     theme:
       active: my-theme
   ```
4. Validate, then run the TUI:
   ```sh
   okt config validate
   okt --project <slug> tui
   ```

If the theme YAML is invalid, the TUI exits with `config_invalid` and the path of the offending file in the error details (`internal/cli/tui.go:loadActiveTheme`).

## Overriding a shipped theme

To tweak a default without forking the file:

1. Copy `<config-root>/themes/omakiten.yaml` to `<config-root>/themes/custom/omakiten.yaml`.
2. Change whatever colors you want.
3. Keep `key: omakiten` and `config.theme.active: omakiten` — the resolver will pick up your custom file because of the `themes/custom/` precedence.

## Bundled themes

Two themes ship in `defaults/themes/`:

| Key | Vibe |
|---|---|
| `omakiten` | Dark with a bright neon-green accent (default). |
| `catppuccin-macchiato` | Catppuccin Macchiato — soft pastels on a deep navy background. |

Inspect either as a starting point — both follow the same eight-token shape plus the conventional `background` / `highlight` keys.
