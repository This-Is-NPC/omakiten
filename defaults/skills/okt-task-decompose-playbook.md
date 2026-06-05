---
name: okt-task-decompose playbook
description: Break a coarse task into right-sized, independently shippable increments.
schema_version: 2
role_affinity:
  - Owner
  - Builder
---
Break a coarse task into right-sized increments. The aim is slices small enough to ship on their own, not a flat checklist.

## Identify the seams

Identify the seams — independently shippable slices, each with its own acceptance criteria — and propose the subtask breakdown rather than creating them blindly.

## Surface the dependencies

Surface the dependencies between slices so they can be ordered into waves later.

## Handoff

Next: suggest `okt-task-estimate` to size each increment.
