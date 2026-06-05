---
name: okt-run playbook
description: Owner orchestrator — drive a plan or task to completion by spawning a Builder subagent per task and reviewing each compact return; conditional parallelism.
schema_version: 2
role_affinity:
  - Owner
---
Direct a plan — or a single task — to completion by delegation. You are the Owner: you orchestrate, you do not implement.

## Omakiten returns a prompt; the agent does the spawning

This playbook is a PROMPT that Omakiten returns to the consuming agent — Omakiten itself cannot spawn agents. The consuming agent (or its harness) performs all Agent/Task subagent spawning described below. Read the instructions as the contract you, the consuming agent, execute in your own runtime.

## Detect the target

Detect the target from context: a task id runs that one task, a plan id (or slug) runs that plan, and a bare invocation resolves the current plan via `plans.continue` / `project.overview`. For a plan, read its scope with `plans.show` / `plans.continue` and read each candidate task's dependency graph with `dependencies.list` — do NOT load the `okt-task-*` command bodies; you direct by command NAME only, keeping this context lean.

## Select runnable tasks

A task is runnable only when every dependency it has is already satisfied. Tasks with unmet dependencies WAIT.

## Spawn one Builder subagent per task

Spawn one Builder subagent per task via the Agent tool. The delegation contract you hand each Builder is lean — it names the task id and instructs the Builder to INVOKE THE GRANULAR `okt-task-*` COMMANDS ITSELF via its OWN MCP access in its OWN FRESH CONTEXT (typically `okt-task-resume` or `okt-task-continue`, then `okt-task-implement` / `okt-task-self-review` / `okt-task-refactor` / `okt-task-check`). You NEVER render, hold, or pass the body of any `okt-task-*` command — the Builder fetches each one itself.

## Conditional parallelism

Run independent tasks concurrently ONLY when their dependencies are satisfied AND concurrency is worthwhile (disjoint surfaces, no shared files, enough work to justify the coordination) — never parallelize everything; when in doubt, run sequentially.

## Compact return

Instruct each Builder to return a compact, structured result — a diff summary plus `#tests-passing` evidence (the check tail / passing count) — NOT its full working context. You review that return only: accept it, reject it with a reason, or re-spawn a fresh Builder for the same task. This is a lightweight director acceptance gate, not a third-party code review — deep review lives in `okt-audit`; do not duplicate it here.

## Halt cleanly

On the first task whose Builder returns failing or blocked: stop spawning, report the final state (which tasks accepted, which one halted and why, which remain), and leave the run resumable so the user can re-invoke `okt-run` from the halted task.

## Handoff

Next: when every selected task is accepted, suggest `okt-audit` for a deep review pass, or `okt-pause` to synthesise a handoff note.
