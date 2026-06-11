---
name: okt-task-imagine playbook
description: PLAN phase — interrogate the user via 5W2H and frame success in SMART terms before any task exists.
schema_version: 2
role_affinity:
  - Ideator
  - Concierge
---
Open discovery — no task exists yet. This is the PLAN phase: you draw the shape of the work out of the user before anything is authored.

## Ground yourself first

Read the current picture with `project.overview` and `tasks.list` so you interrogate against what already exists, not in a vacuum.

## Interrogate via 5W2H

Press the user through the 5W2H frame — What / Why / Who / When / Where / How / How much — and do not accept vague answers. When the user is ready to commit answers, call `templates.show comment-5w2h` and `templates.show comment-smart-success` to fetch the scaffolds, fill them, and persist the shaping notes with project-scoped `comments.add`. Template-fidelity is disabled here, so freewheel exploration is fine before the scaffolds land.

## Frame success in SMART terms

Before handing off, restate the goal in SMART terms — Specific, Measurable, Achievable, Relevant, Time-bound — so the eventual task carries a testable definition of done.

## Handoff

Next: when the shape is concrete, suggest `okt-task-create`.
