# Path Resolution

Canonical write-up of how Omakiten finds its config root, the active yaml profile, and entity overrides. Implementation in `internal/paths/paths.go` and `internal/config/repo_local.go` (walk-up discovery of `.omakiten/`).

## ConfigRoot precedence

In order, first match wins:

1. **`--config <path>` flag** (CLI-level) — pin to a specific yaml file. Skips resolver entirely; the directory containing the file is treated as `<root>/config/`.
2. **<a id="repo-local"></a>Project-local `.omakiten/`** — `config.FindRepoLocal(startDir)` performs the walk-up: starting at the current working directory (CLI) or the project's `root_path`, it ascends looking for a `.omakiten/` directory. The walk stops at `$HOME` and at the filesystem root, so accidental hits in unrelated parents are not picked up. `agentruntime.Open` does **not** discover — it only composes the runtime around whatever path the caller resolved (typically via `FindRepoLocal`). When a `.omakiten/` is found, that directory becomes `<root>` for this invocation — full config + entity layout, distinct per project, committed alongside the repo. SQLite data stays at the user-global path; only the config side is repo-local.
3. **<a id="omakiten-home"></a>`$OMAKITEN_HOME`** env var — pins config, data, state, and entity overrides under one directory. Layout: `$OMAKITEN_HOME/{config,data,state}/...` plus entity folders as siblings.
4. **`$XDG_CONFIG_HOME`** env var — `$XDG_CONFIG_HOME/omakiten`.
5. **OS default** — `~/.config/omakiten` (Linux / macOS); equivalent under Windows.

Repo-local discovery is no-follow: the `.omakiten/` entry and every existing
component from the discovery start directory are checked with `Lstat`. A
symlinked root or intermediate component is an error, not a miss, so a
malicious local tree cannot silently fall through to global configuration.

## `<root>` layout

```text
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

On supported Unix-family targets, the marker, config directory, `custom/`
directory, and selected YAML must be regular non-symlink paths.
`SetActiveConfigInDir` writes `.active` to a same-directory temporary file,
preserves the existing marker mode, flushes the file, closes it, and replaces
the marker with an atomic rename. The Linux and other supported Unix-family
backends hold the directory through descriptor-relative `O_NOFOLLOW`
operations throughout creation, validation, and rename; the marker is
revalidated immediately before commit.

Windows and targets without a native marker backend, including Plan 9, fail
closed for `.active` operations: an existing marker is not read and every
marker write returns an explicit unsupported-backend error. Markerless
alphabetical YAML discovery remains a resolver behavior, but it does not make
setup or TUI marker persistence supported on those targets. On
Windows, `--config <path>` selects a profile without reading or writing
`.active`; Plan 9 and other unsupported targets require a platform support
policy before config installation is supported.

The Linux writer prevents a replacement of the validated directory path from
redirecting the temp file or rename because both are descriptor-relative. A
caller that later opens the returned YAML path can still race an attacker who
replaces that file after discovery; the current editor interfaces accept paths,
not open file handles, so eliminating that final race requires an interface
change rather than more path checks.

## <a id="custom-shadowing"></a>`custom/` shadowing

For both yaml profiles and entity files, the `custom/` subfolder always wins over the root.

- `<root>/laws/custom/my-rule.md` shadows `<root>/laws/my-rule.md` when both have the same slug.
- `<root>/config/custom/my-team.yaml` is preferred over `<root>/config/my-team.yaml` for `.active` resolution.

Rationale: defaults refresh overwrites the root copy on every update; the `custom/` copy survives. Users who want to fork a default copy its file to `custom/` first.

## <a id="boot-order"></a>Boot order

`okt` composition roots (CLI and agentruntime) run, in order:

1. **Compute `rootDir`** directly (`ConfigRoot()` or `ConfigRootFromYAMLPath(--config)`).
2. **`EnsureDefaultFiles(rootDir)`** — seed any missing kit files from the embed.
3. **`ActiveConfigFile()`** — resolve `.active` against the current layout.
4. **`Import(activeConfigPath)`** — load the yaml + apply per-bucket / per-command overrides.

Default-file materialization is supported on Linux, the
supported Unix-family targets, and Windows. Their filesystem backends reject
symlink/reparse-point traversal and use descriptor/handle-relative reads,
writes, removes, and renames. Config file writes preserve the existing
descriptor-relative and atomic replacement guarantees.

Plan 9 and other targets without the safe-I/O backend fail closed before
default materialization; no path-based fallback or marker-persistence
guarantee is made for them.

## <a id="modular-imports"></a>Modular config imports — value-level `from:`

A profile yaml may split sections into separate files and pull them back in with a value-level `from:` directive. The loader expands every directive **before** strict decoding, so the resolved document is decoded and validated exactly as if the imported content had been written inline. Implementation: `internal/config/import_resolver.go` (expansion) and `internal/config/loader.go::readWiringDetailed` (wiring into the strict-decode path).

### What a directive looks like

A directive is a YAML mapping whose **sole** key is `from`, mapping to a relative path:

```yaml
config:
  hooks:
    from: ./hooks.yml
workflows:
  from: ./workflows.yml
```

At load time each `{ from: <path> }` node is replaced **wholesale** by the root node of the referenced document — scalar, sequence, or mapping. So `config.hooks` above becomes whatever `hooks.yml` declares at its root (a list), and top-level `workflows` becomes whatever `workflows.yml` declares (also a list). The directive node disappears entirely from the resolved tree.

### Replacement semantics (v1)

The directive node is **replaced**, not merged. There is no sibling override, no deep merge, and no list append in v1:

- A sole-key `{ from: ./x.yml }` mapping is the only import form. Its replacement is the imported document root, verbatim.
- A mapping that carries `from` **plus other keys** is **not** an import. It passes through untouched and is then decoded normally. This is deliberate: a workflow transition is written `{ from: <bucket>, to: <bucket> }`, and `from` there is an ordinary domain field — flagging it as an import would break every real profile. Only a mapping whose single key is `from` is treated as a directive.
- A mapping that pairs `from` with siblings is fine; a sole-key `from` whose value is empty or non-scalar is a malformed directive and fails the load loudly. There is no "import some keys, override the rest" path.

### Path safety

Path rules mirror the [`subtask_kit` path-safety policy](subtask-kit.md#validator-rules) verbatim (`resolveImportPath` is a copy of `resolveSubtaskKitPath`). A path is resolved relative to the directory of the file that **declared** the directive (nested imports resolve relative to the importing file, not the root profile) and is rejected when it:

- is **absolute**, or
- contains a **parent-directory (`..`) segment**, or
- after symlink resolution, **escapes** the declaring file's directory.

Reads are bounded by the wiring-file budget from `internal/config/size_caps.go` (`MaxWiringFileBytes`); an oversized import surfaces the same coded `ErrConfigTooLarge` as an oversized root profile. On Linux and supported Unix-family targets, the loader opens each declaring directory and imported file with descriptor-relative `openat`/`O_NOFOLLOW` calls. Windows uses native handle-relative no-reparse opens. These backends prevent a path replacement after validation from redirecting the read; Plan 9 and other unsupported targets fail closed rather than using a path-based fallback.

### Nesting, cycles, and depth

Imported documents may themselves contain directives. The resolver walks the whole node tree depth-first:

- **Cycle detection** — a file already on the active import chain is rejected with an `import cycle detected: …` error before it is re-read. A file imported from two distinct branches is allowed (it is decoded once per branch but only listed once as a source).
- **Depth cap** — imports may nest at most **10 levels deep** (`maxImportDepth`; root document is depth 0). A pathologically deep chain is rejected with `import depth exceeds maximum of 10` even if it is acyclic. Ten levels is far beyond any legitimate layout.
- Every error carries the **import chain** (base names joined by `->`) so the failing directive is identifiable without leaking absolute paths.

### Source tracking and hot reload

`Bundle.SourcePaths` lists the root profile first, then every imported file in first-encounter (depth-first) order, each exactly once. When a `subtask_kit:` is wired, its own imports are tracked too. Hot reload watches every entry in `SourcePaths` by mtime, so **editing an imported file triggers the same rebuild as editing the root profile** — there is no separate watch registration for imports.

### Supported scope

Imports are expanded for the **active profile yaml values** only. Entity body/frontmatter loaders (laws, skills, personas, templates, themes, notifications, languages — see [entities.md](entities.md)) do not honor `from:`, but they do use the same bounded no-follow reads. On Linux and supported Unix-family targets, directory enumeration and file reads are pinned to descriptor-relative handles; Windows uses native handle-relative no-reparse opens. Symlinked entity files or custom directories are rejected rather than followed, while Plan 9 and other unsupported targets fail closed. Because expansion happens entirely inside the config loader, **the TUI, CLI, and CLI consume the already-resolved config and need no import awareness** — they see the same materialised `Bundle`/`Snapshot` whether a section was inline or imported.

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

The DB is a single file at `<data-root>/omakiten.db`. A missing file is
initialized from the embedded current schema baseline in
`internal/sqlite/schema.sql`. An existing file must match that baseline
exactly; an older or otherwise mismatched DB is rejected without mutation and
is not migrated. Preserve the rejected file separately if its contents matter,
then either select a new database path, replace it with a current-compatible
backup, or remove it and reinitialize when the old data is disposable. Source:
`internal/paths/paths.go:DataDir`, `DatabaseFile`. The data root is
`$OMAKITEN_HOME/data/`, `$XDG_DATA_HOME/omakiten/`, or
`~/.local/share/omakiten/` in precedence order. See the [current schema and
operational data model](../internal/data-model.md).

## Profiles (advanced)

Multiple yaml profiles can coexist under `<root>/config/`; on supported
Unix-family targets, `<root>/config/.active` names the active one and the TUI
Settings › Config picker writes it. Windows and Plan 9 fail closed when the
picker or resolver would read/write `.active`; see [`.active`
resolution](#active-resolution) above for the support matrix and the
`--config` alternative for explicit profile selection on Windows.

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

The DB file can be copied directly only while every SQLite process is stopped and no WAL sidecar carries committed frames. For the product-supported live snapshot path, use `okt db backup`; it reads through SQLite and includes committed WAL frames without checkpointing the source.

### Rolling snapshots — `okt db backup`

The in-binary `okt db backup` rejects symlink components in both source and destination paths, pins the selected source inode, creates missing destination parents one component at a time, and uses `VACUUM INTO` inside randomized same-parent staging. On Unix the directory/file are created `0700`/`0600`, SQLite binds staging through `/proc/self/fd` or `/dev/fd`, and the destination directory is fsynced. Windows mode bits are not treated as ACL confidentiality guarantees; the rooted creation APIs do not install or validate private DACLs, so deployment policy must provide private native ACL inheritance on the destination directory. Windows holds an identity-checked handle that allows SQLite read/write sharing but denies delete/rename throughout `VACUUM` and verification, flushes the staged file, and skips unsupported read-only-directory `FlushFileBuffers`, so file contents are flushed but directory-entry crash durability is not claimed. No-force publication uses atomic no-replace semantics. `--force` replaces regular files only after retaining the previous inode under a private descriptor-bound rollback link; cleanup or durability failure restores that exact inode and public name. Cleanup errors are surfaced. Rolling retention explicitly protects the newly returned path and uses basename tie-breaking for equal mtimes:

```yaml
config:
  backup:
    retention_count: 5   # keep the 5 newest snapshots; 0 disables prune
```

Project deletion in the CLI and TUI holds a cross-process advisory lease on the backup directory across exact-generation snapshot creation, the SQLite cascade, and rooted retention pruning. The live Store compares `data_version` around the verified image and under `BEGIN IMMEDIATE`, so every row deleted after the writer lock exists in the retained image. Confirmed `okt db reindex` uses the same directory lease. Backup, lease, repeated-generation, or directory-identity failure aborts before destructive commit; the snapshot is the recovery artefact you reach for if a completed cascade went further than expected. `okt update` still uses the shared SQLite-aware backup service before swapping the executable. `okt uninstall` does NOT auto-backup (uninstall removes user-owned data by intent); run `okt db backup` first if you want a snapshot to keep.

The strict snapshot filename pattern (`<yyyy-mm-dd>T<hh-mm-ss.nnnnnnnnn>Z.db`, with the nanosecond suffix optional for older files) means manual `.db` files you drop in the same directory are ignored by the prune pass — only files matching the pattern are rotated.

## Resetting

`mise run purge` removes both `~/.config/omakiten` and `~/.local/share/omakiten` (`.mise.toml`); it does not remove rolling snapshots under `~/.local/state/omakiten`. Re-run `okt init` to reseed defaults. Customs under `<entity>/custom/` are also removed by purge — back them up first if you care.

## <a id="dev-env-layout"></a>Dev-env layout (`dev_env/`)

The local development workflow mirrors the production root under `dev_env/`:

```text
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

`mise run dev:sync` mirrors `defaults/` into `dev_env/` aggressively (managed files overwritten, `custom/` left alone). `dev_env/` itself is gitignored (`.gitignore:24`). The raw-terminal `mise run tui` invokes `dev:install` inside its task body so the nested `dev:sync` + `build` output can be captured without detaching Bubble Tea from its controlling terminal. Both TUI tasks pass an explicit config below `dev_env/`, preventing repo-local `.omakiten/` discovery from escaping the dev environment.

## Update when

- `internal/paths/paths.go` adds or changes a path-resolution helper (new env var, new layout shape).
- `internal/config/repo_local.go` changes the `.omakiten/` walk-up behavior.
- The `okt config <sub>` surface grows or renames a subcommand.
- Backup filename pattern or retention semantics shift.
- A new top-level folder lands under `<root>/` or `dev_env/`.
- `internal/config/import_resolver.go` changes the `from:` directive contract, path-safety policy, depth cap, or cycle handling.

## See also

- [system.md](system.md) — `config.backup` retention knob and other runtime config.
- [data-model.md](../internal/data-model.md) — current SQLite schema and operational data.
- [subtask-kit.md](subtask-kit.md) — the `subtask_kit:` cascade, whose path-safety policy the `from:` import resolver reuses.
- [project-overrides.md](project-overrides.md) — per-project layering (the architecture above the on-disk layout).
- `internal/paths/paths.go`, `internal/config/repo_local.go`, `internal/config/loader.go`, `internal/config/import_resolver.go` — implementation.
