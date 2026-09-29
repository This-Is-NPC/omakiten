# Presets — side-by-side comparison

Omakiten offers four official workflow repositories. Inspect their metadata with
`okt preset catalog`, then choose one with `okt setup --preset <name>` or
`okt config init --scope local --preset <name>`. Each repository owns its workflow,
entities, settings and version. See [configuration-guide/README.md](configuration-guide/README.md)
for schemas and [command-surface.md](command-surface.md) for command roles.

| Preset | Repository |
| --- | --- |
| `omakase` | `https://github.com/This-Is-NPC/okt-workflow-omakase.git` |
| `izakaya` | `https://github.com/This-Is-NPC/okt-workflow-izakaya.git` |
| `kaiseki` | `https://github.com/This-Is-NPC/okt-workflow-kaiseki.git` |
| `shokunin` | `https://github.com/This-Is-NPC/okt-workflow-shokunin.git` |

The binary carries catalog metadata and an Omakase fixture for development and
verification. Installation reads the chosen repository. Git and repository access
are required for a first installation by name or URL; a local directory also works.
Once installed, runtime and preset selection work offline. Setup updates pristine
snapshots from their recorded source and preserves the active modified preset.

## Repository packages

A preset repository is a self-contained package. Install and select it in the
current project:

```sh
okt preset catalog
okt preset add omakase
# A Git URL or /path/to/okt-workflow-omakase also works
okt preset use omakase
okt config validate
```

Use `--scope global` on `add`, `use`, `list`, or `import` to operate on the
user configuration. `--project` and `--project-id` select the project root for
local operations. Installing a package does not activate it; `use` selects an
installed name or full content id. `list` reports each snapshot's id, manifest,
active status, and modification status. Select by id when a name has multiple
installed revisions.

The repository contains `preset.yaml` and its files:

```yaml
schema_version: 1
name: omakase
version: 0.1.0
config: config/preset.yaml
```

`config/preset.yaml` links modules with the existing `from` directive:

```yaml
version: 1
kit: {id: 101, key: omakase, name: Omakase Workflow Preset}
config: {from: ./settings.yaml}
workflows: {from: ./workflows.yaml}
surfaces: {from: ./surfaces.yaml}
skills: {from: ./catalog.yaml#skills}
laws: {from: ./catalog.yaml#laws}
personas: {from: ./personas.yaml}
commands: {from: ./bindings.yaml}
```

Fixture exports create these modules. Settings reference separate
`views.yaml`, `events.yaml`, and `hooks.yaml` modules. Short scalar lists and
simple records use compact YAML rows; nested rules remain indented blocks.
Package edits update the files that own changed values, preserving imports
and comments. Unchanged modules retain their exact bytes and permissions.

The config references sibling `skills/`, `laws/`, `personas/`, `templates/`,
`themes/`, and `notifications/` directories. Additional assets,
including hook scripts, belong in the same repository. Application languages and
preferences are owned by Omakiten; see [languages.md](configuration-guide/languages.md). Entity files live
directly in their folders. Package paths are canonical relative paths; files
must be regular files. Installation validates the manifest, config schema,
references, and file integrity. It does not analyze script code or execute
hooks during installation.

Installation stores a complete snapshot under `<scope>/presets/<id>/`.
`<scope>/config.yaml` selects its config through a relative path. The project
scope is `.omakiten/`; the global scope uses the usual configuration root.
Runtime loading uses the installed files, so the source checkout is not
required after installation. Operational tasks and plans remain in SQLite.

Editing an active package through CLI or Studio stages the complete package,
validates the candidate, installs its snapshot, and atomically updates the
selection. The original remains available. The first content change creates
`<name>-local`; subsequent edits retain that name. `dirty` compares the files
and config entry with the recorded origin hash. A failed edit leaves the
active selection intact. Use the selection file for edits rather than editing
an installed snapshot directly.

### Single-file transport

```sh
okt preset export --output omakase.md
okt preset import --file omakase.md
okt preset use omakase
```

Export includes the manifest and every file, preserving script bytes and
executable permissions. It writes one Markdown document with strict YAML
frontmatter. `export` writes Markdown to stdout by default; `import --file -`
reads stdin. Existing output files require `--force`. `--name` and `--version`
override export metadata. Import installs without activating. The package
and its encoded document are limited to 16 MiB. Repository installation accepts catalog names, Git URLs and local directories.
Remote checkouts are temporary and are deleted after their files are captured.

## At a glance

| Preset | Discipline | Buckets | Forward guards | Best for |
|---|---|---|---|---|
| **🍻 izakaya** | Lean spike / tracer-bullet / walking skeleton | `backlog → dev → done` | `#hypothesis`, `wave_gate` on `backlog→dev` only | Solo experiments, prototypes, time-boxed spikes. |
| **🍱 omakase** | Trunk-based development + DORA + TDD | `backlog → dev → review → done` | `#self-branch`, `blockers_in`, `#resume`, `wave_gate`, `subtasks_complete` | Small teams shipping continuously to main. |
| **🎌 kaiseki** | Staged delivery with formal sign-offs | `requirements → planning → dev → review → docs → done` | Requirements, acceptance, design, peer review, documentation, `subtasks_complete` | Regulated work, multi-stakeholder coordination. |
| **🥢 shokunin** | SRE + pre-mortem + multi-reviewer change control | `requirements → planning → dev → review → docs → done` | Kaiseki plus pre-mortem, risk assessment, rollback plan, dual review, lessons learned | Production-critical paths where blast radius matters. |

## How presets differ

Per-preset workflow behavior is declared in each repository's `config/workflows.yaml`. What changes by discipline level is:

- **`workflows[].buckets` / `transitions` / `operations`** — bucket count, forward gates, regression paths, and destructive-operation guards.
- **Guard vocabulary** — each discipline level adds checks without prescribing code architecture.

Inspect any preset's full wiring when you need exact active slugs:

```bash
okt preset catalog                          # repository metadata
okt config init --preset izakaya --scope local --force
okt config show --scope local                # render the active file
```

## Picking a preset

| You want to… | Use |
|---|---|
| Spike a feature in a day, throw away half. | **izakaya** |
| Ship to main multiple times a week with TDD + green CI. | **omakase** |
| Cross stage gates with documented hand-offs and sign-offs. | **kaiseki** |
| Run production-critical work with pre-mortem + multi-reviewer. | **shokunin** |

## Switching between presets

Install a workflow package and select its snapshot in the current scope:

```bash
okt preset add kaiseki
okt preset use kaiseki
okt config validate
```

Existing tasks keep their `bucket_id` — but bucket ids are workflow-scoped. Switching presets does NOT migrate task state. Re-bucket via `okt move <id> --to <new-bucket>` per task, or absorb the work as a one-time migration before the switch.

For smaller changes, prefer editing the relevant config module over whole-preset switching. See [configuration-guide/README.md](configuration-guide/README.md).

## Update when

- A new official workflow repository is added to the catalog — add a row to [At a glance](#at-a-glance) and a guidance entry to [Picking a preset](#picking-a-preset).
- A preset's bucket count or guard set changes meaningfully (sanity-check the row in the comparison table).
- The selection guidance shifts (new discipline, new vocabulary).

## See also

- [workflow.md](workflow.md) — conceptual loop and per-preset walkthrough.
- [command-surface.md](command-surface.md) — stable command roles, scopes, and write behavior.
- [configuration-guide/workflows.md](configuration-guide/workflows.md) — workflow schema.
- [configuration-guide/command-bindings.md](configuration-guide/command-bindings.md) — command role/skill/law/template binding schema.
- [configuration-guide/guards.md](configuration-guide/guards.md) — the guard types these presets compose.
- Each workflow repository's `config/workflows.yaml` — its buckets and guards.
