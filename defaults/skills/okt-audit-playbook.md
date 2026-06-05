---
name: okt-audit playbook
description: "Owner orchestrator — commission a deep assurance pass: spawn Reviewer + Security subagents, aggregate severity-tagged findings, coach on risk."
schema_version: 2
role_affinity:
  - Owner
  - Reviewer
---
Commission an assurance pass on completed work. You are the director: you do not perform the review yourself — you delegate to subagents, one per assurance lens, and aggregate what they return.

## Omakiten returns a prompt; the agent does the spawning

This playbook is a PROMPT that Omakiten returns to the consuming agent — Omakiten itself cannot spawn agents. The consuming agent (or its harness) performs all Agent/Task subagent spawning described below. Read the instructions as the contract you, the consuming agent, execute in your own runtime.

## Detect the target

Detect the target from context: a task id audits that task's diff, a plan id audits every task the plan completed; a bare invocation audits the current branch's diff resolved via `project.overview` / `plans.continue`.

## Spawn subagents

Spawn a Reviewer subagent and a Security subagent in parallel via the Agent tool — their surfaces are disjoint, so concurrency is worthwhile. The delegation contract you hand each is lean: it names the target and instructs the subagent to INVOKE THE GRANULAR COMMANDS ITSELF via its OWN MCP access in its OWN FRESH CONTEXT — the Reviewer runs `okt-task-review` then `okt-task-quality`; the Security subagent runs `okt-task-secure`. You NEVER render or hold those command bodies; each subagent fetches its own.

## Run the playbook: review → secure → quality → debrief

The review and secure passes run inside the spawned subagents; once their findings land you commission the quality read and close with `okt-task-debrief` to capture what the audit learned.

## Aggregate the findings

Aggregate the findings into one report, each finding SEVERITY-TAGGED (`error` / `warning` / `info`) and attributed to its lens; de-duplicate overlaps where the Reviewer and Security subagent flagged the same line.

## Coach on severity and risk

Rank by blast radius, not count — one `error` on an auth path outweighs a dozen `info` smells; call out which findings block ship versus which are follow-ups, and say so plainly. This is the deep third-party review pass `okt-run` deliberately does NOT do — do not collapse it back into a director acceptance gate.

## Handoff

Next: route blocking findings to `okt-task-implement` with the finding id and location, or suggest `okt-pause` to record a handoff note when the audit clears.
