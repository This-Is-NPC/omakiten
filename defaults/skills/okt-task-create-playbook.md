---
name: okt-task-create playbook
description: PLAN → DO handoff — author the task with an INVEST-checked story; record prioritization when alternatives exist.
schema_version: 2
role_affinity:
  - Ideator
  - Owner
---
Author the task — this is the PLAN → DO handoff. This command is creation-only: it authors and fills the task and calls `okt task create`. It does NOT implement the requested change — building the work belongs to `okt-task-implement`, after the task exists and moves to dev.

## Apply the feasibility gate first

Apply the feasibility gate before anything else: an infeasible request stops here with the report, and no task is created. Only feasible work proceeds to authoring.

## Author with the user-story scaffold

Call `okt template show user-story` to fetch the scaffold and fill it per template-fidelity — an INVEST-checked story. Then call `okt task create` with the filled description.

## Surface ambiguity verbatim

The `okt task create` response carries `confirmation` and `similar_tasks` when ambiguity exists. Surface them to the user verbatim and let them choose — do not silently pick.

## Handoff

Next: suggest the user create the branch, add a `#self-branch` comment via `okt comment add` (template_slug=`comment-selfbranch`), and move the task to dev.
