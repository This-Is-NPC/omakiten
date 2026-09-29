---
name: Config Orientation
description: Map the selected package, application preferences, and workflow modules.
entity: orientation
laws:
  - template-fidelity
---

# Configuration orientation

## Find the selected scope

Read `okt config path`, `okt config show`, and `okt config why KEY` for the
intended project. An explicit `--config` wins; project-local `.omakiten/`
selection otherwise takes precedence over the user installation.

## Separate the owners

| Owner | State |
| --- | --- |
| Application | Language preferences and embedded translation catalogs. |
| Preset package | Settings, workflows, surfaces, entities, bindings, themes, notifications, and scripts. |
| Database | Projects, tasks, plans, comments, relationships, events, errors, and solutions. |

`config.yaml` selects an installed preset snapshot under `presets/<id>/`.
The root package manifest names its config entry. `from` imports a value;
`merge_from` merges a mapping. Follow the modules that own the selected keys.

## Edit and validate

Entity commands and Studio stage a complete candidate package and publish it
only after validation. The first content change creates a modified local
preset. File-level policy changes belong in the source package, followed by
validation, installation, and explicit activation.

Use `okt config language show`, `set`, and `reset` for application language.
Use `okt workflow show` for active policy and `workflow orphans` before
rebinding work after a bucket or kit change.

## Inspect agent instructions

`okt command list` discovers configured command skills. `okt command resolve NAME`
composes the bound persona, skills, laws, and templates without executing them.

## Read more

- `docs/how-to-configure-a-project.md` — scope, discovery, and runtime ownership.
- `docs/how-to-manage-presets.md` — snapshots, activation, and transport.
- `docs/how-to-customize-a-workflow.md` — modules, entities, permissions, and guards.
- `docs/how-to-use-with-agents.md` — context recovery and instruction resolution.
- `docs/internal/data-model.md` — operational state and document metadata.
