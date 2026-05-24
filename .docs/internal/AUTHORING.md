# Authoring Omakiten Docs

Single contract for editing this repo's documentation. If a change to docs feels like it forces edits in three or more files, you are probably violating one of the rules below — stop and re-read.

## Four hard rules

1. **Every atom of information has one canonical home.** Other docs reference it by anchor link (preferred) or by an auto-include marker (when the snippet must render inline). Copying the text is forbidden.
2. **Aggregate views are auto-generated.** Entity catalogs, per-preset wiring snapshots, tag vocabulary, and similar listings come from `mise run docs:refresh`. Never hand-edit them.
3. **`.docs/_generated/` is off-limits for hand edits.** Every generated file carries a `<!-- GENERATED ... -->` header. Edit the canonical source instead — frontmatter under `defaults/{laws,skills,personas,templates}/` or yaml under `defaults/config/`.
4. **CHANGELOG is one line per entry plus a PR link.** Detail lives in the PR body, never in the CHANGELOG prose.

## Atom map — where each fact lives

| Atom | Canonical home | Where it surfaces (auto) |
|---|---|---|
| Law / skill / persona / template description | `defaults/<kind>/<slug>.md` frontmatter | `.docs/_generated/entities-<kind>.md` |
| Theme colors and TUI palette | `defaults/themes/<slug>.yaml` | linked from `.docs/theming-guide.md` |
| Notification card definitions (kitten_*, etc.) | `defaults/notifications/<slug>.yaml` | linked from `.docs/notifications.md` |
| Bundled language packs (21 CLI/TUI locales) | `defaults/languages/<code>.yaml` | linked from `.docs/languages-guide.md`; parity test enforces key set |
| Per-preset wiring (personas allowlist, mcp_commands, workflow guards) | `defaults/config/<preset>.yaml` | `.docs/_generated/presets-<preset>.md` |
| Tag vocabulary (`#self-branch`, `#resume`, `#5w2h`, etc.) | walked from `comments_tagged.tag` in preset yamls | `.docs/_generated/tag-vocabulary.md` |
| Bibliography citations (Beck, Cockburn, Cagan, Klein, …) | `.docs/reference/bibliography.md` | linked by anchor |
| Mental models (PDCA / 5W2H / SMART / INVEST / MoSCoW / RICE / OKR) | `.docs/explanation/mental-models.md` | linked by anchor |
| Path resolution semantics | `.docs/reference/path-resolution.md` (canon: `internal/paths/paths.go`) | linked by anchor |
| Filesystem layout tree | `.docs/reference/layout.md` | linked by anchor |
| Requirements catalog | `.docs/internal/requirements.md` | linked |

## Cross-reference patterns

### Pattern 1 — anchor link (default; ~80% of cases)

```markdown
The omakase preset uses the [`comments_tagged`](../guards-guide.md#comments_tagged) guard to require a `#review` comment before leaving `dev`.
```

Renders in any markdown viewer. Anchor breakage is caught by the `markdown-link-check` lint (when wired in CI).

### Pattern 2 — auto-include marker (snippet must render inline)

```markdown
<!-- BEGIN include:_generated/presets-omakase.md#workflow-guards -->
...rewritten by mise run docs:refresh from .docs/_generated/presets-omakase.md...
<!-- END include -->
```

Use when an agent reads the doc from an MCP prompt and cannot follow links (e.g. `config-orientation.md`). The body between markers is replaced on every `docs:refresh`.

Include can target a whole file (`include:_generated/foo.md`) or a section (`include:_generated/foo.md#section-name`). Sections in generated files are wrapped with `<!-- SECTION:name -->` / `<!-- END SECTION -->`.

### Pattern 3 — auto-catalog (large entity tables)

```markdown
<!-- BEGIN auto:catalog kind=laws -->
...table rewritten by mise run docs:refresh from .docs/_generated/entities-laws.md...
<!-- END auto:catalog -->
```

Valid `kind` values: `laws`, `skills`, `personas`, `templates`.

### Pattern 4 — central glossary lookup

`explanation/mental-models.md` defines each model once with a stable anchor. Every other doc cites `[INVEST](../explanation/mental-models.md#invest)`.

## CI gate

```
go run ./cmd/okt-docs-refresh --root . --check
```

Runs as part of `mise run check` and as a step in `.github/workflows/ci.yml`. Fails if regenerating would produce a non-empty diff. Failure message names the drifting files; the fix is to run `mise run docs:refresh` locally and commit the result.

## Workflow for adding a new entity (canonical: a new law)

1. Create `defaults/laws/<slug>.md` with `name`, `severity`, and a body that explains the rule.
2. Wire the law into the relevant preset under `defaults/config/<preset>.yaml` (`mcp_commands.<command>.laws`).
3. Add one line to `CHANGELOG.md` under the next `## [Unreleased]` block with a PR link.
4. Run `mise run docs:refresh` — generated catalogs and marker blocks update in-place.
5. Run `mise run check` — lint, vet, tests, and the docs-drift gate must all stay green.

Three hand-edited files total: the entity, the preset yaml, the CHANGELOG line. Everything else regenerates.

## Workflow for adding a bundled language pack

Languages do not slot into the law/skill/persona/template recipe because each pack is a flat key/value YAML (no frontmatter, no preset wiring) and the file count is much larger.

1. `scripts/new-language-pack.sh <code> "<native>" "<English name>"` — scaffolds `defaults/languages/<code>.yaml` with TODO markers on every value; the English baseline is preserved so the parity test passes from the first commit.
2. Translate values, preferably one logical surface per commit (CLI → TUI → notifications). The fixed primitives (`workflow`, `bucket`, `task`, `comment`, …) stay in English per [Languages Guide § Preserve primitives in ASCII](../languages-guide.md#preserve-primitives-in-ascii).
3. Add one line to `CHANGELOG.md` per language under `## [Unreleased]`.
4. Run `mise run check` — the parity test (`internal/config/language_pack_parity_test.go`) catches missing or extra keys.

No code change needed: the installer picker auto-discovers anything under `defaults/languages/`. Custom (non-bundled) packs go in `~/.config/omakiten/languages/custom/<code>.yaml` instead. Full contract in the [Languages Guide](../languages-guide.md).

## Token / size budgets (kept for reuse in entity bodies)

| Kind | Body budget | Notes |
|---|---|---|
| Law | ≤120 tokens | Rule + one bad/good example pair max |
| Skill | ≤80 tokens | Steps or rules; no narrative |
| Persona | ≤200 tokens | Voice + loop only; no architecture prescription |
| Template | ≤250 tokens | Placeholder scaffold only |

These limits exist because every law/skill/persona/template that a preset wires into a prompt expands inline. The agent's context budget is finite.

## What NOT to prescribe in workflow entities

Process discipline, yes. Architecture, no. Reviewer ergonomics, yes. Specific frameworks (Clean / Hexagonal / DDD / MVC), no. Best practices (TDD, peer review, decision records), yes. Specific tooling (Jest / pytest / Datadog), no.

The kit ships four official presets — `omakase`, `izakaya`, `kaiseki`, `shokunin` — each a distinct **process discipline**. None prescribe architecture.
