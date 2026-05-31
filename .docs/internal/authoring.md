# Authoring docs in this tree

Three rules govern edits to `.docs/`:

1. **Every atom of information has one canonical home.** Other docs reference it by anchor link. Copying the text is forbidden.
2. **Each doc declares "Update when".** Look for the "Update when" section at the bottom of every doc — that names the trigger that should bring you back to edit it.
3. **No catalogs Omakiten can derive from code.** Domain events, tag vocabulary, concrete entity catalogs, and CLI flag dumps should never be hand-maintained. The source files (`internal/domain/event.go`, `defaults/config/<preset>.yaml`, entity folders, etc.) are the source of truth — link to them and document the wiring logic instead.

## Atom map — where each fact lives

| Atom | Canonical home |
|---|---|
| Law / skill / persona / template description | `defaults/<kind>/<slug>.md` frontmatter (inspect via `okt <kind> show <slug>`) |
| Theme colors and TUI palette | `defaults/themes/<slug>.yaml` (linked from `configuration-guide/themes.md`) |
| Notification card definitions | `defaults/notifications/<slug>.yaml` (linked from `configuration-guide/notifications.md`) |
| Bundled language packs | `defaults/languages/<code>.yaml` (linked from `configuration-guide/languages.md`) |
| Per-preset wiring | `defaults/config/<preset>.yaml` (compared in `presets.md`) |
| Tag vocabulary | walked from `comments_tagged.tag` in preset yamls — no static doc |
| Domain events | `internal/domain/event.go::KnownEventTypes` (cross-referenced from hooks / events docs) |
| Mental models + citations | `why_omakiten.md` (canonical) |
| Filesystem layout + path resolution | `configuration-guide/path-resolution.md` (canonical: `internal/paths/paths.go`) |

## Token / size budgets (kept for reuse in entity bodies)

| Kind | Body budget | Notes |
|---|---|---|
| Law | ≤120 tokens | Rule + one bad/good example pair max |
| Skill | ≤80 tokens | Steps or rules; no narrative |
| Persona | ≤200 tokens | Voice + loop only; no architecture prescription |
| Template | ≤250 tokens | Placeholder scaffold only |

These limits exist because every law/skill/persona/template a preset wires into a prompt expands inline. The agent's context budget is finite.

### What NOT to prescribe in workflow entities

Process discipline, yes. Architecture, no. Reviewer ergonomics, yes. Specific frameworks (Clean / Hexagonal / DDD / MVC), no. Best practices (TDD, peer review, decision records), yes. Specific tooling (Jest / pytest / Datadog), no.

The kit ships four official presets — `omakase`, `izakaya`, `kaiseki`, `shokunin` — each a distinct **process discipline**. None prescribe architecture.

### Workflow for adding a new entity (canonical: a new law)

1. Create `defaults/laws/<slug>.md` with `name`, `severity`, and a body that explains the rule.
2. Wire the law into the relevant preset under `defaults/config/<preset>.yaml` (`mcp_commands.<command>.laws`).
3. Run `mise run check` — lint, vet, tests must stay green.

Two hand-edited files total: the entity and the preset yaml. Release notes come from the PR / release-please flow.

### Workflow for adding a bundled language pack

See [../configuration-guide/languages.md](../configuration-guide/languages.md) — `scripts/new-language-pack.sh` scaffolds the file with TODO markers and the parity test catches drift.
