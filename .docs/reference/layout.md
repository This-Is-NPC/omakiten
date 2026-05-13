# Filesystem Layout

Canonical directory tree for an Omakiten installation. Anchored sections — link from other docs as `[layout](./reference/layout.md#root-tree)`.

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
└── themes/                       same shape
    ├── <slug>.md
    └── custom/<slug>.md
```

Data — the SQLite database that backs every project — lives under a parallel root rather than alongside `config/`:

```
$OMAKITEN_HOME/data/omakiten.db        (when OMAKITEN_HOME is set)
$XDG_DATA_HOME/omakiten/omakiten.db    (when XDG_DATA_HOME is set)
~/.local/share/omakiten/omakiten.db    (default per platform)
```

## <a id="custom-shadowing"></a>`custom/` shadowing

Every entity directory (`config/`, `laws/`, `skills/`, `personas/`, `templates/`, `themes/`) has a `custom/` subdirectory. For each slug, `custom/` always wins.

Rationale: `mise run install` (and `okt config sync`) overwrites the root copy of every kit-shipped entity. The `custom/` copy is never touched. Users who fork a default copy its file to `custom/` first; agents respect the override transparently.

## <a id="dev-env-layout"></a>Dev-env layout (`dev_env/`)

The local development workflow mirrors the production root under `dev_env/`:

```
<repo>/dev_env/
├── config/
├── laws/
├── skills/
├── personas/
├── templates/
└── themes/
```

`mise run dev:sync` mirrors `defaults/` into `dev_env/` aggressively (root overwritten, `custom/` left alone). Tasks that need a clean dev state (`mise run mcp:prompts`, `mise run tui`) `depends = ["dev:sync"]` first.

## See also

- [`reference/path-resolution.md`](./path-resolution.md) — how Omakiten finds `<root>` and the active profile.
- [`AUTHORING.md`](../AUTHORING.md) — `_generated/` rule and atom-map for docs under `.docs/`.
- [`configuration-guide.md`](../configuration-guide.md) — yaml field reference.
