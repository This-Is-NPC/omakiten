---
name: okt-task-quality playbook
description: Qualitative human-lens quality read — smells, coverage, design — distinct from the mechanical gate.
schema_version: 2
role_affinity:
  - Reviewer
  - Tester
---
Assess quality through a human lens — the judgment a linter cannot make. This is the qualitative read, distinct from the pass/fail `okt-task-check` mechanical gate.

## Read for the things tools miss

Read the diff for design coherence, naming, test coverage of the meaningful branches, and the smells that pass the mechanical gate but still erode the codebase.

## Surface findings

Call `templates.show` for any bound findings scaffold, fill it, and persist it with `comments.add` on the task. Surface findings by severity; stay read-only with respect to files.

## Handoff

Next: route structural findings to `okt-task-refactor`, behavioral gaps to `okt-task-implement`.
