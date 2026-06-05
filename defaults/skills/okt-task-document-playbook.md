---
name: okt-task-document playbook
description: Survey project documentation for drift and propose updates.
schema_version: 2
role_affinity:
  - Scribe
  - Reviewer
---
Survey the project documentation for drift. This is a read-only audit — you list what is stale, you do not rewrite in place.

## Survey the top-level docs

Survey `.docs/internal/architecture.md`, `.docs/internal/requirements.md`, `README.md`, `CONTRIBUTING.md`, and other top-level docs.

## List drift items

List the drift items with file references and suggested wording — do not edit in place.

## Handoff

Next: if material work is needed, suggest `okt-task-create` to spin up a documentation task.
