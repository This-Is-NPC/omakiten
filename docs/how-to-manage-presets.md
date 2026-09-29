# How to choose and share a workflow

**The question:** how do I use a workflow repository, keep a customized copy,
and move it to another Omakiten installation?

A preset is a package of policy and assets. Its tasks do not live in the
package. Installing a repository captures its files; you do not need to keep
the source checkout synchronized for the installed workflow to run.

## 1. Pick a starting point

```bash
okt preset catalog
```

| Preset | Starting discipline | Repository |
| --- | --- | --- |
| `izakaya` | Short experiments and prototypes | [okt-workflow-izakaya](https://github.com/This-Is-NPC/okt-workflow-izakaya) |
| `omakase` | Development, review, and completion | [okt-workflow-omakase](https://github.com/This-Is-NPC/okt-workflow-omakase) |
| `kaiseki` | Requirements and planning before delivery | [okt-workflow-kaiseki](https://github.com/This-Is-NPC/okt-workflow-kaiseki) |
| `shokunin` | Risk evidence and stricter review | [okt-workflow-shokunin](https://github.com/This-Is-NPC/okt-workflow-shokunin) |

These are repository sources, each with its own version and declared policy.
Inspect the installed workflow for the exact buckets and guards; names alone
do not describe every rule.

## 2. Install, then select

```bash
okt --project example preset add omakase
okt --project example preset list
okt --project example preset use omakase
okt --project example config validate
```

`add` accepts a catalog name, Git URL, or local package directory. It installs
without activating. `use` selects an installed name or complete content id;
use the id when several revisions have the same name.

The default scope is local to the project. `--scope global` on scoped preset
commands operates on the user installation. Global setup can select a preset
with `okt setup --preset omakase`.

Fetching a repository needs Git and access to that source. Runtime use and
activation of captured snapshots work offline. Remote checkout directories
are temporary and are discarded after capture.

## What is in a repository

```text
preset.yaml
config/
  preset.yaml
  settings.yaml
  workflows.yaml
  surfaces.yaml
  catalog.yaml
  personas.yaml
  bindings.yaml
skills/
laws/
personas/
templates/
themes/
notifications/
```

The root manifest identifies the package:

```yaml
schema_version: 1
name: example
version: 0.1.0
config: config/preset.yaml
```

Additional files, including scripts for hooks, can belong to the package.
Paths must be canonical relative paths and files must be regular files.
Installation validates the manifest, configuration, references, and file
integrity. Script content is not inspected or executed during installation.

Language translations and application language preferences belong to Omakiten.
They are not preset assets.

## 3. Keep your changes as a preset

Edit through the entity commands or Studio. The editor stages the package,
validates the candidate, installs a new snapshot, and updates the selection
atomically. A failed edit leaves the selected snapshot intact.

The first content change creates `<name>-local`; subsequent edits keep that
name. `preset list` reports whether the package differs from its recorded
origin. The original snapshot remains selectable.

For changes to YAML policy, edit a source repository, validate its config
entry, then install that repository and select the resulting snapshot.
[Changing a workflow](how-to-customize-a-workflow.md) covers the modules.

## 4. Carry it as one file

```bash
okt --project example preset export --output workflow.md
okt --project example preset import --file workflow.md
okt --project example preset use example
```

Export includes the manifest and every package file, including executable
permissions. Import installs without activating. The transport is one Markdown
document with strict YAML frontmatter, limited to 16 MiB.

Export writes Markdown to stdout when no output path is supplied. Import
accepts `--file -` for stdin. `--force` replaces an existing output file;
`--name` and `--version` can change export metadata.

## Before switching a board with existing work

Tasks retain their bucket identities. Selecting another preset does not
migrate them. Run `okt workflow orphans` to inspect tasks whose buckets no
longer exist, then use its documented rebind options deliberately. Do this
before assuming that a successful `preset use` means the old work fits the
new workflow.
