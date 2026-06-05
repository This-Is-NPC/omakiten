---
name: okt-help playbook
description: "System command — tier-aware guide to how omakiten works: the orchestrator/system/granular tiers, the start→shape→run→audit→pause flow, and when to drop to granular commands."
schema_version: 2
role_affinity:
  - Concierge
---
Orient the user on how omakiten works and help them organize their work — this is the tutorial, not a project action. Teach the surface in three layers.

## The command tiers

Omakiten's `okt-*` commands fall into three tiers, and naming the tier tells the user which altitude they are operating at:

- ORCHESTRATORS (bare, primary path): `okt-start`, `okt-shape`, `okt-run`, `okt-audit`, `okt-pause` (and the bare `okt`, a shortcut to `okt-start`). These are the director commands — they read state, propose the next move, and delegate the surgical work. Reach for these first; they are where most sessions live.
- SYSTEM (bare, talk to the TOOL not the project): `okt-help` (this), `okt-config` (orient on / customize the config + environment), `okt-skill <slug>` (load a skill body, or list the catalog). No project object — they configure or explain omakiten itself.
- GRANULAR (object-namespaced `okt-<object>-<verb>`): the power-user, surgical surface — `okt-task-*`, `okt-plan-*`, `okt-project-*`, `okt-note-*`. One precise step each (implement, review, secure, claim, …).

## The mental flow

A session normally walks `okt-start` → `okt-shape` → `okt-run` → `okt-audit` → `okt-pause`: START to orient and pick up the prior thread, SHAPE a raw idea or loose backlog into ready tasks plus a plan, RUN to drive that plan to completion by delegation, AUDIT for a deep assurance pass, then PAUSE to snapshot a handoff note for the next session. The orchestrators each name the next command, so the flow self-advances.

## When to drop to granular

Stay on the orchestrators for the normal path; drop to the granular okt-task-* / okt-plan-* commands when you need a single surgical step the orchestrator would otherwise delegate: building one task by hand (`okt-task-continue` → `okt-task-implement`), running just a review (`okt-task-review`), a security-only pass (`okt-task-secure`), or claiming one plan task (`okt-plan-claim`). Rule of thumb: orchestrators decide and delegate; granulars do the one thing — reach for a granular when you already know the exact step and want to skip the director.

## Handoff

Next: suggest `okt-start` to begin a session, `okt-config` to customize the environment, or `okt-skill` to browse the available skills.
