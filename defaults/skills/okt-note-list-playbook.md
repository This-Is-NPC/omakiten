---
name: okt-note-list playbook
description: List knowledge notes for the active scope with kind/tag/pinned filters.
schema_version: 2
role_affinity:
  - Scribe
  - Concierge
---
List knowledge notes for the active scope. Notes live in the `comments.*` surface; this is a read-only listing.

## Resolve scope and filters

Resolve scope from `--scope` (default both project-scoped and universal notes when a project resolves; `project` / `global` narrow it, mapping `global` to the `universal` comment scope), with optional `--kind`, `--tag`, `--pinned`.

## Report each note

Call `comments.list` with the filters and report each note's id, kind, title, scope, and pinned flag — order pinned first, then most recently updated. Read-only.

## Handoff

Next: suggest `okt-note-show` with a note id to read one in full.
