---
name: okt-skill playbook
description: System command — load a skill body via skills.get (e.g. okt-skill commit), or list the catalog via skills.list with no arg; pulls any skill, ungated by persona repertoire.
schema_version: 2
role_affinity:
  - Concierge
---
Load a skill on demand, or browse the skill catalog. Resolve the slug from `--slug` or the first positional argument (e.g. `/okt-skill commit`).

## With a slug

Call `skills.get` for that slug and surface the skill's full BODY verbatim — the procedural payload the user asked to read (e.g. `/okt-skill commit` loads the `commit` skill body via `skills.get`). When the slug is unknown, `skills.get` rejects naming the missing slug — relay that and suggest a bare `okt-skill` to see the valid slugs.

## With no argument

Call `skills.list` and render the catalog — every loaded skill's slug + name + description, ordered by slug — so the user can pick one to load.

## Ungated by the persona repertoire

This command pulls ANY skill in the catalog: it is NOT gated by the active persona's skill repertoire. The repertoire only decides which skills auto-flow into a command's prompt; `okt-skill` is the explicit escape hatch to read any authored skill on demand, regardless of which persona is bound. Read-only — skills are authored by the user; never create, edit, or delete a skill through this command.

## Handoff

Next: when the loaded skill names a process step, suggest the matching granular command (e.g. the `commit` skill → `okt-task-commit`); otherwise suggest `okt-help` for the command tour.
