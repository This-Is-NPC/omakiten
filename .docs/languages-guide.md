# Languages Guide — adding a bundled pack

This guide walks a contributor through shipping a new bundled language pack so it auto-appears in `okt setup`, validates under `okt config language`, and passes the parity test on the first commit. It is lang-code-agnostic — substitute your target code anywhere `<code>` appears below.

## What ships today

Twenty-one packs live under `defaults/languages/`:

```
defaults/languages/
  en.yaml           # English — baseline; the parity test uses this as the source of truth
  es.yaml           # Spanish
  pt-br.yaml        # Portuguese (Brazil)
  jp.yaml           # Japanese
  fr.yaml           # French
  de.yaml           # German
  ru.yaml           # Russian
  zh-cn.yaml        # Chinese (Simplified)
  ko.yaml           # Korean
  ar.yaml           # Arabic
  hi.yaml           # Hindi
  mr.yaml           # Marathi
  tr.yaml           # Turkish
  it.yaml           # Italian
  pl.yaml           # Polish
  nl.yaml           # Dutch
  da.yaml           # Danish
  fi.yaml           # Finnish
  no.yaml           # Norwegian
  sv.yaml           # Swedish
  uk.yaml           # Ukrainian
```

The CLI/TUI surface picks the active pack via `config.languages.{cli,tui}` in the active `omakiten.yaml`. Whatever language codes the installer sees under `defaults/languages/` at build time appear automatically in the `okt setup` picker and pass `okt config language set --cli <code>` validation — there is no allowlist to update.

Every bundled pack ships fully translated for the CLI surface; longer TUI strings and notification voice may still lean on the English fallback in a few keys, which is acceptable per the parity rule (parity is structural, not content). A follow-up PR replacing those fallbacks is always welcome — the [smoke recipe](#end-to-end-smoke-recipe) below shows how to verify locally.

Codes commonly requested but **not yet bundled** at the time of writing: `vi` (Vietnamese), `zh-tw` (Chinese, Traditional). PRs for any of them are welcome; pick whichever matters to you and open one per language.

## Filename convention

`defaults/languages/<code>.yaml`. The `<code>` segment is the lowercase BCP-47 subset used everywhere else in the runtime:

- Lowercase letters only, optionally followed by a single `-` separator and a country/script tag.
- **Hyphen, not underscore**: `pt-br`, `zh-cn`, `zh-tw` (never `pt_br`).
- One file per locale. Country variants are separate packs (`pt-br` and a future `pt-pt` would coexist).

The basename (everything before `.yaml`) is the literal value passed to `--cli-lang`, `--tui-lang`, and `OKT_CLI_LANG` / `OKT_TUI_LANG` — keep the casing exact.

## Header fields

The first three lines of every pack are the same:

```yaml
code: pt-br
name: Portuguese (Brazil)
native: Português (Brasil)
keys:
  ...
```

- `code` — must match the filename without `.yaml`. The loader rejects mismatches.
- `name` — English display name. Used by `okt config language show` and the CLI flag error text.
- `native` — the endonym (what speakers call the language themselves). Used in the picker row label, rendered as `<native> (<code>)`. Use the actual script: `日本語`, `한국어`, `Português (Brasil)`, `العربية`, `हिन्दी`.

After the three header keys comes `keys:`, a flat map of dotted-path keys to translated strings. Order under `keys:` is irrelevant — the loader rebuilds a map.

## Translation conventions

These rules were settled in task #82 / task #119 and are enforced informally by review. The parity test only checks key parity, not content; conventions live here as the authoring contract.

### Preserve primitives in ASCII

Omakiten's domain vocabulary stays in English even inside translations. Translating them creates ambiguity (a German `Workflow` translated to `Arbeitsablauf` would not match the CLI flag `--workflow`).

The fixed primitives:

```
workflow · preset · bucket · harness · skill · law · persona · agent · MCP ·
TUI · CLI · tag · blocker · slug · scope · task · comment · frontmatter ·
hot-reload · cascade · severity
```

Translate verbs, adjectives, UI labels, sentence connectors — keep the primitives.

> Good (pt-br): `"Cria uma task no projeto ativo"` — verb `Cria` translated, primitive `task` and `projeto` (Portuguese) kept.
>
> Bad: `"Cria uma tarefa no projeto ativo"` — `tarefa` does not match the CLI primitive `task`.

### Preserve placeholders

Every `%s`, `%d`, `%q`, `%v` is consumed by `fmt.Sprintf` at runtime. Drop one and the binary panics on first use. Reorder them only if the target language requires it and the format verb count stays identical.

```
en:    "Moved #%d to %s"
pt-br: "Movida #%d para %s"   # same order, same verbs
```

### Notification voice

The `notifications.{izakaya,kaiseki,omakase,shokunin}.*.message` and `notifications.kitten_*.description` keys carry a deliberate thematic voice:

- **izakaya** — casual bar slang. Patrons, tabs, bouncers.
- **kaiseki** — formal kitchen brigade. Courses, plating, service notes.
- **omakase** — RPG quest log. Quests, runes, ravens, the bestiary.
- **shokunin** — workshop / inspection. Specimens, stations, audit trail.
- **kitten_*** — the kitten persona that surfaces in the desktop notifications.

Keep the thematic register when translating. A literal translation that erases the voice is worse than a creative one that keeps it. Aim for the same emotional shape your bartender / sous-chef / quest-giver would use in the target language.

### Markdown / multiline values

Keys like `cli.root.long` and `cli.config.init.long` use the YAML block scalar `|-`. Preserve the literal newlines and indentation — they render as the help text the user reads. Backticks, `<angle>` examples, and code-fence blocks pass through unchanged.

## Parity rule

`internal/config/language_pack_parity_test.go::TestBundledLanguagePacksHaveIdenticalKeySets` walks every YAML under `defaults/languages/` and enforces that each pack declares **exactly the same key set** as `en.yaml`. The test reports both directions:

- *missing N keys* — the pack lacks an `en` key. At runtime that key silently falls back to `en` (acceptable behavior but not authorial intent — translate it).
- *N extra keys* — the pack has a key `en` does not. Dead translation effort; either delete the extra key or add it to `en` first.

Failure mode looks like:

```
--- FAIL: TestBundledLanguagePacksHaveIdenticalKeySets
    language_pack_parity_test.go:48: language pack pt-br missing 3 keys (first 5):
        [tui.kicker.totals tui.kicker.tokens tui.stat.total_row_label]
```

Recipe to fix:

1. Open `defaults/languages/en.yaml`, search for each missing key.
2. Add the same key to the failing pack with a translated value (or copy the English value temporarily and mark it `# TODO(translate)` above the line — the parity test passes either way; only the value differs).
3. Re-run `go test ./internal/config -run TestBundledLanguagePacksHaveIdenticalKeySets`.

The reverse (`N extra keys`) usually means a key was renamed in `en.yaml` and the pack still carries the old one — delete the stale key.

## Custom packs (user-level, not bundled)

End users can drop their own packs under `~/.config/omakiten/languages/custom/<code>.yaml` and `okt config language set --cli <code>` will pick them up. Custom-wins dedup applies: a custom pack with `code: pt-br` shadows the bundled `pt-br.yaml`. YAML decode failures on a custom pack are logged to stderr and the file is skipped — boot does not abort.

This guide is for **bundled** packs (shipped inside the binary). Custom packs are the right answer for one-off forks, in-house corporate translations, or experiments before opening a PR.

## Scaffolding helper

`scripts/new-language-pack.sh <code> <native> <name>` copies `defaults/languages/en.yaml` to `defaults/languages/<code>.yaml` with:

- the three header fields swapped to your inputs,
- a `# TODO(translate): <key>` line comment above every translated value.

```sh
# scaffold an Italian pack
scripts/new-language-pack.sh it Italiano Italian
```

The English value is preserved on every line, so the parity test stays green from the first commit. Translate values incrementally; remove each `# TODO(translate)` marker as you go. The companion test (`internal/config/language_pack_scaffold_test.go::TestNewLanguagePackScript`) exercises the script against a throwaway `zz-test` code on every `go test ./internal/config` run, so the scaffold contract cannot silently regress.

## End-to-end smoke recipe

After scaffolding and translating (even partially):

```sh
# 1. Verify parity + decode.
go test ./internal/config -run TestBundledLanguagePacksHaveIdenticalKeySets

# 2. Full suite + lint + vuln + docs check.
mise run check

# 3. Headless picker (replace <code> with your code).
OKT_CLI_LANG=<code> OKT_TUI_LANG=<code> OKT_PRESET=omakase OKT_HARNESSES=0 \
  go run ./cmd/okt setup --skip-wrapper --skip-harnesses

# 4. Manual TUI smoke.
go run ./cmd/okt config language set --cli <code> --tui <code>
go run ./cmd/okt tui
```

Step 3 fails loudly with `--%s %q is not a loaded language code` if the file is malformed or the loader rejected it; the parity test would have caught most of those already.

`scripts/sync-defaults.sh` includes `languages` in its sync loop, so `mise run install` refreshes the bundled packs in the user-global install on every dev run.

## Worked example — adding Vietnamese

```sh
scripts/new-language-pack.sh vi "Tiếng Việt" Vietnamese
# → defaults/languages/vi.yaml created with TODO markers on every value.

# Translate as much as you can. Commit per logical surface (CLI / TUI / notifications)
# to keep diffs reviewable.
git add defaults/languages/vi.yaml
git commit -m "feat(i18n): bundle Vietnamese (vi) — CLI surface translated, TUI + notifications pending"

# Re-run check before pushing.
mise run check
```

A first PR that translates only the CLI surface is fine. The remaining keys still resolve via the English value (silent fallback). Follow-up PRs land the TUI surface, then notifications. Each PR is small enough to review in one sitting.

## Where to look in the code

- `defaults/languages/en.yaml` — baseline key set; the source of truth for the parity test.
- `internal/config/language_pack_parity_test.go` — parity rule plus the test that catches drift.
- `internal/config/language_pack_scaffold_test.go` — exercises `scripts/new-language-pack.sh`.
- `internal/cli/setup_picker.go::loadBundledLanguageOptions` — combines embedded packs (via sibling `loadEmbedLanguageOptions`, which auto-discovers via `defaults.FS.ReadDir("languages")`) with user customs.
- `internal/cli/setup_picker.go::loadCustomLanguageOptions` — merges `~/.config/omakiten/languages/custom/` on top.
- `scripts/sync-defaults.sh` — copies packs into the user-global install on `mise run install`.

That is the entire mechanism. No Go code changes are required to add a new bundled language — just the YAML file and (optionally) a refresh of any cross-referencing doc.
