---
name: okt-config playbook
description: System command — orient on the active Omakiten config layout to customize the environment.
schema_version: 2
role_affinity:
  - Concierge
---
Orient the user on the active config layout so they can customize their omakiten environment.

## Load the orientation scaffold

Call `templates.show config-orientation` to load the path resolution order, entity layout, frontmatter shapes, wiring relationships, and workflow guard kinds. Read it fully before answering any config-edit question — do not guess.

## What the config governs

The config is where the user tailors omakiten: the active preset and workflow, the personas/laws/skills/templates each `okt-*` command binds, the agent output language, and the workflow guard rules. Editing an entity file or `omakiten.yaml` reshapes how every command resolves, so locate the exact file before proposing a change.

## Handoff

Next: for the broader tour of how the command tiers fit together, suggest `okt-help`; when the user has a concrete edit in mind, suggest `okt-task-implement` with the change scoped to `omakiten.yaml` or the relevant entity file.
