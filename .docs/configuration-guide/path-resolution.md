# Path Resolution

Canonical write-up of how Omakiten finds its config root, the active yaml profile, and entity overrides. Implementation in `internal/paths/paths.go` and `internal/config/repo_local.go` (walk-up discovery of `.omakiten/`).

## ConfigRoot precedence

In order, first match wins:

1. **`--config <path>` flag** (CLI-level) — pin to a specific yaml file. Skips resolver entirely; the directory containing the file is treated as `<root>/config/`.
2. **<a id="repo-local"></a>Project-local `.omakiten/`** — `config.FindRepoLocal(startDir)` performs the walk-up: starting at the current working directory (CLI / MCP) or the project's `root_path`, it ascends looking for a `.omakiten/` directory. The walk stops at `$HOME` and at the filesystem root, so accidental hits in unrelated parents are not picked up. `agentruntime.Open` does **not** discover — it only composes the runtime around whatever path the caller resolved (typically via `FindRepoLocal`). When a `.omakiten/` is found, that directory becomes `<root>` for this invocation — full config + entity layout, distinct per project, committed alongside the repo. SQLite data stays at the user-global path; only the config side is repo-local.
3. **<a id="omakiten-home"></a>`$OMAKITEN_HOME`** env var — pins config, data, state, and entity overrides under one directory. Layout: `$OMAKITEN_HOME/{config,data,state}/...` plus entity folders as siblings.
4. **`$XDG_CONFIG_HOME`** env var — `$XDG_CONFIG_HOME/omakiten`.
5. **OS default** — `~/.config/omakiten` (Linux / macOS); equivalent under Windows.

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
├── themes/<slug>.yaml
├── themes/custom/<slug>.yaml
├── notifications/<slug>.yaml
├── notifications/custom/<slug>.yaml
├── languages/<code>.yaml
└── languages/custom/<code>.yaml
```

Data (SQLite db) lives under a parallel root: `$OMAKITEN_HOME/data/` or `$XDG_DATA_HOME/omakiten/` or `~/.local/share/omakiten/`. Recoverable state, currently database backups, lives under `$OMAKITEN_HOME/state/` or `$XDG_STATE_HOME/omakiten/` or `~/.local/state/omakiten/`.

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

`ConfigRootFromYAMLPath` never returns an error. Anywhere else falls back to the parent directory of the yaml file — the caller gets a usable `<root>` even for unrecognized layouts, and downstream resolution (`.active`, entity lookup) decides whether that root is viable.

## Inspecting the active layer — `okt config <sub>`

| Subcommand | Purpose |
| --- | --- |
| `okt config init --scope <global\|local> --preset <name> [--force]` | Materialise a complete install (config + entity folders + preset library) into the chosen scope. `--force` re-copies every embedded shipped file; user `custom/` subtrees are never touched. |
| `okt config show --scope <global\|local>` | Print the raw bytes of the chosen scope's active yaml. |
| `okt config path --scope <global\|local>` | Print the install root directory (the ConfigRoot for global, the discovered `.omakiten/` for local). |
| `okt config why <key> [--layer <global\|local>]` | Walk the active config (or a pinned layer) by dotted YAML key path and report `{key, value, source, path}`. Missing keys return `source = "not_set"`. |
| `okt config diff <left> <right>` | Structural YAML diff between two sources. Operands accept `global`, `local`, `local:<path>`, or any raw yaml file path. Emits one entry per divergent leaf (`added` / `removed` / `changed`). |

## TUI scope badge

Settings › General shows a `scope` row that reads:
- `global` — runtime is loading the user-global install.
- `local (<.omakiten path>)` — runtime is loading a discovered repo-local install.

The badge reflects what the loader actually picked, not the discovery candidates. Using `--config <path>` clears the badge to `global` because the explicit flag bypasses walk-up discovery.

## SQLite database

The DB is a single file at `<data-root>/omakiten.db`. Schema migrations are applied transactionally on every connect (`internal/sqlite/store.go:Open`). Source: `internal/paths/paths.go:DataDir`, `DatabaseFile`. The data root is `$OMAKITEN_HOME/data/`, `$XDG_DATA_HOME/omakiten/`, or `~/.local/share/omakiten/` in precedence order.

## Profiles (advanced)

Multiple yaml profiles can coexist under `<root>/config/`; `<root>/config/.active` names the active one and the TUI Settings › Config picker writes it. See [`.active` resolution](#active-resolution) above for the full custom-before-root, alphabetical fallthrough order.

## Backups

Everything Omakiten persists is on the local filesystem. Config, data, and recoverable state are separate paths:

```sh
# Config (yaml + markdown entities + YAML assets + custom overrides)
cp -a "${OMAKITEN_HOME:-$HOME/.config/omakiten}" /backup/omakiten-config

# Data (SQLite) when OMAKITEN_HOME is unset.
cp -a "${XDG_DATA_HOME:-$HOME/.local/share}/omakiten" /backup/omakiten-data

# Recoverable state (rolling DB snapshots) when OMAKITEN_HOME is unset.
cp -a "${XDG_STATE_HOME:-$HOME/.local/state}/omakiten" /backup/omakiten-state

# Data + recoverable state when OMAKITEN_HOME is set.
cp -a "$OMAKITEN_HOME/data" /backup/omakiten-data
cp -a "$OMAKITEN_HOME/state" /backup/omakiten-state
```

The DB file can be copied while `okt` is not running. For the product-supported snapshot path, use `okt db backup`; it performs an atomic file copy and is the only backup flow Omakiten wraps in CLI/TUI behavior today. There is no concurrent multi-writer story — the tool is single-user, single-process by design.

### Rolling snapshots — `okt db backup`

The in-binary `okt db backup` writes the live SQLite file to a timestamped snapshot under `$XDG_STATE_HOME/omakiten/backups/<utc-iso>.db` (defaults to `~/.local/state/omakiten/backups/`; under `$OMAKITEN_HOME`, `$OMAKITEN_HOME/state/backups/`). The copy is atomic (tmp + rename) and prunes older snapshots according to `config.backup.retention_count`:

```yaml
config:
  backup:
    retention_count: 5   # keep the 5 newest snapshots; 0 disables prune
```

Every destructive command — `okt projects delete`, `okt update`, the TUI Home `d`+`d` confirm — runs the same routine before mutating state. Backup failure aborts the destructive flow with a coded error; the snapshot is the recovery artefact you reach for if the cascade went further than expected. `okt uninstall` does NOT auto-backup (uninstall removes user-owned data by intent); run `okt db backup` first if you want a snapshot to keep.

The strict snapshot filename pattern (`<yyyy-mm-dd>T<hh-mm-ss.nnnnnnnnn>Z.db`, with the nanosecond suffix optional for older files) means manual `.db` files you drop in the same directory are ignored by the prune pass — only files matching the pattern are rotated.

## Resetting

`mise run purge` removes both `~/.config/omakiten` and `~/.local/share/omakiten` (`.mise.toml`); it does not remove rolling snapshots under `~/.local/state/omakiten`. Re-run `okt init` to reseed defaults. Customs under `<entity>/custom/` are also removed by purge — back them up first if you care.

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

`mise run dev:sync` mirrors `defaults/` into `dev_env/` aggressively (root overwritten, `custom/` left alone). `dev_env/` itself is gitignored (`.gitignore:24`). Tasks that need a clean dev state pull it in differently: `mise run mcp:prompts` `depends = ["dev:sync"]` directly, while `mise run tui` `depends = ["dev:install"]`, which transitively chains `dev:sync` + `build` before running `okt setup` against `dev_env/`.

## Update when

- `internal/paths/paths.go` adds or changes a path-resolution helper (new env var, new layout shape).
- `internal/config/repo_local.go` changes the `.omakiten/` walk-up behavior.
- The `okt config <sub>` surface grows or renames a subcommand.
- Backup filename pattern or retention semantics shift.
- A new top-level folder lands under `<root>/` or `dev_env/`.

## See also

- [system.md](system.md) — `config.backup` retention knob and other runtime config.
- [project-overrides.md](project-overrides.md) — per-project layering (the architecture above the on-disk layout).
- `internal/paths/paths.go`, `internal/config/repo_local.go`, `internal/config/loader.go` — implementation.
