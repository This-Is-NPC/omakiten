# Path Resolution

Canonical write-up of how Omakiten finds its config root, the active yaml profile, and entity overrides. Implementation in `internal/paths/paths.go`.

## ConfigRoot precedence

In order, first match wins:

1. **`--config <path>` flag** (CLI-level) — pin to a specific yaml file. Skips resolver entirely; the directory containing the file is treated as `<root>/config/`.
2. **<a id="omakiten-home"></a>`$OMAKITEN_HOME`** env var — pins config + data + entity overrides under one directory. Layout: `$OMAKITEN_HOME/{config,data}/...` plus entity folders as siblings.
3. **`$XDG_CONFIG_HOME`** env var — `$XDG_CONFIG_HOME/omakiten`.
4. **OS default** — `~/.config/omakiten` (Linux / macOS); equivalent under Windows.

## `<root>` layout

```
<root>/
├── config/
│   ├── .active               one-line state file naming the active profile
│   ├── omakase.yaml          official kit (default canonical)
│   ├── izakaya.yaml          official kit
│   ├── kaiseki.yaml          official kit
│   ├── shokunin.yaml         official kit
│   └── custom/               user-owned profiles (survive defaults refresh)
│       └── my-team.yaml
├── laws/<slug>.md            default entries; overwritten on update
├── laws/custom/<slug>.md     user-created entries; preserved
├── skills/...                same shape as laws/
├── personas/...
├── templates/...
└── themes/...
```

Data (SQLite db) lives under a parallel root: `$OMAKITEN_HOME/data/` or `$XDG_DATA_HOME/omakiten/` or `~/.local/share/omakiten/`.

## <a id="active-resolution"></a>`.active` resolution

The `<root>/config/.active` state file stores the basename of the currently selected profile yaml (e.g. `omakase.yaml`).

`ActiveConfigFile()` resolves this name into an absolute path with the following precedence:

1. If `.active` exists and is non-empty:
   - Try `<root>/config/custom/<name>` first. Return if it exists.
   - Otherwise try `<root>/config/<name>`. Return if it exists.
   - **<a id="fallthrough"></a>Fallthrough**: if the named profile is missing from both, fall through to discovery (step 2) instead of returning a stale path. Prevents a removed-or-renamed canonical kit from breaking init.
2. **Discovery** — return the first `.yaml` (alphabetical) in:
   - `<root>/config/` (root before custom/)
   - `<root>/config/custom/` (only if the root has none)

Errors with `no config yaml found in …` only when no `.yaml` exists anywhere — a config file is mandatory.

## <a id="custom-shadowing"></a>`custom/` shadowing

For both yaml profiles and entity files, the `custom/` subfolder always wins over the root.

- `<root>/laws/custom/my-rule.md` shadows `<root>/laws/my-rule.md` when both have the same slug.
- `<root>/config/custom/my-team.yaml` is preferred over `<root>/config/my-team.yaml` for `.active` resolution.

Rationale: defaults refresh overwrites the root copy on every update; the `custom/` copy survives. Users who want to fork a default copy its file to `custom/` first.

## <a id="boot-order"></a>Boot order

`okt` composition roots (CLI and agentruntime) run, in order:

1. **Compute `rootDir`** directly (`ConfigRoot()` or `ConfigRootFromYAMLPath(--config)`).
2. **`MigrateLayout(rootDir)`** — move legacy layouts forward (e.g. relocate a root-level `omakiten.yaml` into `custom/` once the canonical kit renames).
3. **`EnsureDefaultFiles(rootDir)`** — seed any missing kit files from the embed.
4. **`ActiveConfigFile()`** — resolve `.active` against the **post-migration** layout. Honors any rename that step 2 just performed.
5. **`Import(activeConfigPath)`** — load the yaml + apply per-bucket / per-command overrides.

The migrate-before-resolve order matters: when a previous canonical kit name (`omakiten.yaml`) gets moved into `custom/`, the post-migration resolver finds it there via the custom-before-root precedence. Pre-migration resolution would point at the now-empty root and error at `Import` with `config_invalid`.

## <a id="config-root-from-yaml-path"></a>`ConfigRootFromYAMLPath` recognized shapes

When a `--config` flag points at a yaml file, the resolver derives `<root>` from its path. Recognized shapes:

| Yaml path | Recognized `<root>` |
|---|---|
| `<root>/config/<file>.yaml` | `<root>` (canonical) |
| `<root>/config/custom/<file>.yaml` | `<root>` (custom shadowed) |
| `<root>/<file>.yaml` (legacy flat layout) | `<root>` |

Anywhere else returns an error — the file must live under a recognized `config/` parent (with or without `custom/`).

## See also

- [`integration-guide.md`](../integration-guide.md) — wiring hooks against a resolved profile.
- [`configuration-guide.md`](../configuration-guide.md) — yaml field reference.
- `internal/paths/paths.go` and `internal/config/loader.go` — implementation.
