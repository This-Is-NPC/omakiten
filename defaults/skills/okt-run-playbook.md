---
name: okt-run playbook
description: Owner orchestrator — drive a plan or task to completion by spawning a Builder subagent per task and reviewing each compact return; conditional parallelism.
schema_version: 2
role_affinity:
  - Owner
---
Direct a plan — or a single task — to completion by delegation. You are the Owner: you orchestrate, you do not implement. Do not call `okt-start` from here; `okt-run` is already the driver.

## Omakiten returns a prompt; the agent does the spawning

This playbook is a PROMPT that Omakiten returns to the consuming agent — Omakiten itself cannot spawn agents. The consuming agent (or its harness) performs all Agent/Task subagent spawning described below. Read the instructions as the contract you, the consuming agent, execute in your own runtime.

## Detect the target

Detect the target from context: a task id runs that one task, a plan id (or slug) runs that plan, and a bare invocation resolves the current plan via `plans.continue` / `project.overview` / `plans.list`. If no runnable target is identifiable, stop and ask for a task id or plan slug; do not fall back to `okt-start` and do not summarize the board as a substitute for running. For a plan, read its scope with `plans.show` / `plans.continue` and read each candidate task's dependency graph with `dependencies.list` — do NOT load the `okt-task-*` command bodies; you direct by command NAME only, keeping this context lean.

## Select runnable tasks

A task is runnable only when every dependency it has is already satisfied. Tasks with unmet dependencies WAIT.

## Claim plan work before delegation

For a plan target, acquire work with `plans.claim_next` before spawning a Builder. Repeat claims only while the active wave still has claimable tasks and concurrency is worthwhile; every Builder receives a claimed task id, never an unclaimed task copied from `plans.show`. The claim is ownership only: after claim, the Builder must satisfy preset guards and use `tasks.move` for bucket transitions.

## Keep the plan current

The board is the source of truth during a plan run — code in git without Omakiten bookkeeping is incomplete.

For **each task** you finish (whether you implemented inline or accepted a Builder return):

1. Record progress on **that task** with `progress.record` or `comments.add` — not only on the umbrella/parent.
2. Move **that task** through the workflow with `tasks.move` when guards are satisfied (`#self-branch` before dev, `#resume` + `#tests-passing` before review, `#documentation` before done). Respect `dependencies.list` — every blocker must be `done` first.
3. Call `plans.show` and confirm `done_count` / `percent` advanced before starting the next task.

When **all plan tasks are `done`**: check off the umbrella acceptance criteria, move the umbrella to `done`, and call `plans.edit` with `status: done` plus a refreshed `goal_body` delivery table.

If you implement inline without spawning Builders, you still perform steps 1–3 per task — claiming or coding does not replace workflow moves.

## Spawn one Builder subagent per task

Spawn one Builder subagent per task via the Agent tool. The delegation contract you hand each Builder is lean — it names the claimed task id and instructs the Builder to INVOKE THE GRANULAR `okt-task-*` COMMANDS ITSELF via its OWN MCP access in its OWN FRESH CONTEXT (typically `okt-task-resume` or `okt-task-continue`, then `okt-task-implement` / `okt-task-self-review` / `okt-task-refactor` / `okt-task-check`). Require the Builder to persist material progress with `progress.record` or `comments.add` and to use `tasks.move` for workflow transitions when guards are satisfied. You NEVER render, hold, or pass the body of any `okt-task-*` command — the Builder fetches each one itself.

## Conditional parallelism

Run independent tasks concurrently ONLY when their dependencies are satisfied AND concurrency is worthwhile (disjoint surfaces, no shared files, enough work to justify the coordination) — never parallelize everything; when in doubt, run sequentially.

## Compact return

Instruct each Builder to return a compact, structured result — a diff summary plus `#tests-passing` evidence (the check tail / passing count) — NOT its full working context. You review that return only: accept it, reject it with a reason, or re-spawn a fresh Builder for the same task. This is a lightweight director acceptance gate, not a third-party code review — deep review lives in `okt-audit`; do not duplicate it here.

## Halt cleanly

On the first task whose Builder returns failing or blocked: stop spawning, record the halt with `progress.record` or a task comment, report the final state (which tasks accepted, which one halted and why, which remain), and leave the run resumable so the user can re-invoke `okt-run` from the halted task.

## Handoff

Next: when every selected task is accepted, suggest `okt-audit` for a deep review pass, or `okt-pause` to synthesise a handoff note.
