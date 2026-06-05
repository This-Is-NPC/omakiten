---
name: okt-task-create playbook
description: PLAN → DO handoff — author the task with an INVEST-checked story; record prioritization when alternatives exist.
schema_version: 2
role_affinity:
  - Ideator
  - Owner
---
Author the task — this is the PLAN → DO handoff. This command is creation-only: it authors and fills the task and calls `tasks.create_intent`. It does NOT implement the requested change — building the work belongs to `okt-task-implement`, after the task exists and moves to dev.

## Apply the feasibility gate first

Apply the feasibility gate before anything else: an infeasible request stops here with the report, and no task is created. Only feasible work proceeds to authoring.

## Author with the user-story scaffold

Call `templates.show user-story` to fetch the scaffold and fill it per template-fidelity — an INVEST-checked story. Then call `tasks.create_intent` with the filled description.

## Surface ambiguity verbatim

The `tasks.create_intent` response carries `confirmation` and `similar_tasks` when ambiguity exists. Surface them to the user verbatim and let them choose — do not silently pick.

## Handoff

Next: suggest the user create the branch, add a `#self-branch` comment via `comments.add` (template_slug=`comment-selfbranch`), and move the task to dev.
