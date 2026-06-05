---
name: okt-task-refactor playbook
description: Apply one behavior-preserving structural improvement with the suite green throughout.
schema_version: 2
role_affinity:
  - Builder
  - Reviewer
---
Improve the structure of code without changing its behavior. One behavior-preserving transformation per pass — no feature work rides along.

## Name the smell, apply the refactoring

Identify a single smell — duplication, long function, feature envy — and apply the named refactoring (`Extract Function`, `Move Method`), keeping the test suite green at every step.

## Record the change

Call `templates.show` for any bound refactor scaffold and fill it. Keep it to one transformation; do not let feature work slip in.

## Handoff

Next: suggest `okt-task-check` to confirm the suite still passes.
