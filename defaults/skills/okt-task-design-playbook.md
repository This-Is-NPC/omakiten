---
name: okt-task-design playbook
description: Shape the solution — approach, seams, interfaces — and weigh an alternative before coding.
schema_version: 2
role_affinity:
  - Builder
  - Reviewer
---
Shape the solution before writing it. Design is where the trade-offs are made cheaply, on paper, before code locks them in.

## Sketch the approach

Sketch the approach — the data flow, the seams you will touch, the interfaces you will introduce — and weigh at least one alternative against it.

## Record the rationale

Call `templates.show` for any bound design scaffold, fill it, and persist the design rationale with `comments.add` on the task. Do not edit production code here.

## Handoff

Next: suggest `okt-task-implement` to build the chosen design.
