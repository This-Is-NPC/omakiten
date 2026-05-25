# Filesystem Layout

Canonical directory tree for an Omakiten installation. Anchored sections — link from `.docs/*.md` files as `[layout](./reference/layout.md#root-tree)`, from nested docs as `[layout](../reference/layout.md#root-tree)`, or from this folder as `[layout](./layout.md#root-tree)`.

## <a id="root-tree"></a>Root tree

```
<root>/                          resolved per .docs/reference/path-resolution.md
├── config/
│   ├── .active                  one-line state file (basename of active profile)
│   ├── omakase.yaml             official preset — Trunk-based + DORA + TDD
│   ├── izakaya.yaml             official preset — Lean spike + tracer bullet
│   ├── kaiseki.yaml             official preset — Staged delivery + sign-offs
│   ├── shokunin.yaml            official preset — SRE + pre-mortem + audit trail
│   └── custom/                  user-owned profiles (survive defaults refresh)
│       └── <my-profile>.yaml
├── laws/
│   ├── <slug>.md                default entry (overwritten on defaults sync)
│   └── custom/<slug>.md         user-created entry (preserved)
├── skills/                       same shape as laws/
│   ├── <slug>.md
│   └── custom/<slug>.md
├── personas/                     same shape
│   ├── <slug>.md
│   └── custom/<slug>.md
├── templates/                    same shape
│   ├── <slug>.md
│   └── custom/<slug>.md
├── themes/                       TUI theme definitions
│   ├── <slug>.yaml
│   └── custom/<slug>.yaml
├── notifications/                kit notification cards (kitten_*, plus preset
│   ├── <slug>.yaml               personas) — referenced from `config.hooks`;
│   └── custom/<slug>.yaml        same custom/ shadowing as the other folders.
└── languages/                    bundled CLI/TUI language packs (one yaml per
    ├── <code>.yaml               BCP-47 code; 21 today: en, es, pt-br, jp, fr,
    └── custom/<code>.yaml        de, ru, zh-cn, ko, ar, hi, mr, tr, it, pl, nl,
                                  da, fi, no, sv, uk). Custom packs live under
                                  `custom/` and shadow same-coded bundled packs.
```

Data — the SQLite database that backs every project — lives under a parallel root rather than alongside `config/`:

```
$OMAKITEN_HOME/data/omakiten.db        (when OMAKITEN_HOME is set)
$XDG_DATA_HOME/omakiten/omakiten.db    (when XDG_DATA_HOME is set)
~/.local/share/omakiten/omakiten.db    (default per platform)
```

## <a id="custom-shadowing"></a>`custom/` shadowing

Every entity directory (`config/`, `laws/`, `skills/`, `personas/`, `templates/`, `themes/`, `notifications/`, `languages/`) has a `custom/` subdirectory. For each slug or language code, `custom/` always wins.

Rationale: `mise run install`, `okt setup --update`, and `okt config init --force` overwrite the root copy of kit-shipped assets. The `custom/` copy is never touched. Users who fork a default copy its file to `custom/` first; agents respect the override transparently.

## <a id="repo-local-layout"></a>Project-local layout (`.omakiten/`)

When a project commits its own `.omakiten/` directory at the repo root (or anywhere up the path tree under `$HOME`), `config.FindRepoLocal` resolves that directory as `<root>` for every invocation made from inside that tree. The internal shape mirrors the user-global root verbatim — `config/`, `laws/`, `skills/`, `personas/`, `templates/`, `themes/`, `notifications/`, and `languages/`, each with its own `custom/` shadow folder:

```
<repo>/.omakiten/
├── config/
│   ├── .active
│   ├── omakase.yaml
│   └── custom/
├── laws/
│   ├── <slug>.md
│   └── custom/<slug>.md
├── skills/...
├── personas/...
├── templates/...
├── themes/...
├── notifications/...
└── languages/...
```

This is the per-project install — pinned config, pinned entity overrides, version-controlled with the code. The SQLite database stays at the user-global path (`~/.local/share/omakiten/omakiten.db` or the `$XDG_DATA_HOME` / `$OMAKITEN_HOME` equivalents); only the read side is repo-local. Walk-up discovery stops at `$HOME` and the filesystem root, so the feature opt-in is precise: no `.omakiten/` in the path tree → fall through to the global resolver. See [`path-resolution.md` § Project-local `.omakiten/`](./path-resolution.md#repo-local).

## <a id="dev-env-layout"></a>Dev-env layout (`dev_env/`)

The local development workflow mirrors the production root under `dev_env/`:

```
<repo>/dev_env/
├── config/
├── laws/
├── skills/
├── personas/
├── templates/
├── themes/
├── notifications/
└── languages/
```

`mise run dev:sync` mirrors `defaults/` into `dev_env/` aggressively (root overwritten, `custom/` left alone). Tasks that need a clean dev state (`mise run mcp:prompts`, `mise run tui`) `depends = ["dev:sync"]` first.

## See also

- [`reference/path-resolution.md`](./path-resolution.md) — how Omakiten finds `<root>` and the active profile.
- [`AUTHORING.md`](../internal/AUTHORING.md) — `_generated/` rule and atom-map for docs under `.docs/`.
- [`configuration-guide.md`](../configuration-guide.md) — yaml field reference.
