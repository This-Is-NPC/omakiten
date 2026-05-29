---
name: Meaning closed loop
description: "Verify the delivered work answers the original ask before handing back: restate intent, map it to evidence, surface any scope shift explicitly."
schema_version: 2
role_affinity:
  - Builder
  - Reviewer
  - Scribe
---
A unit of work starts as an intent — a question the requester wants answered or an outcome they want to exist. The loop closes only when the hand-back demonstrates that the delivered change satisfies that exact intent. This skill is the procedure for closing it; the `meaning-closed-loop` law is the obligation to do so.

## Restate the intent

- Quote or paraphrase the original ask in one sentence, in the requester's terms. If the task carried acceptance criteria, the intent is the union of those criteria — not your summary of them.
- If you cannot restate the intent crisply, the loop cannot close. Ask before building, rather than answering a question you guessed at.

## Map intent to evidence

- For each acceptance item or each clause of the ask, point to the concrete evidence that satisfies it: a test, a rendered output, a file path, an observed behaviour.
- Answer the question that was asked. When the ask is "what is missing?", deliver the list; do not substitute counts, percentages, or a breakdown of an adjacent dimension.
- Distinguish "done and verified" from "done, unverified" from "deferred". An item with no evidence is not closed.

## Surface scope shifts

- If the work diverged from the original intent — a constraint forced a different approach, the ask turned out to be under-specified, a sub-goal proved out of scope — state the shift explicitly and record the revised intent.
- An unstated shift is the most common way the loop silently breaks: the requester reads "done", assumes their intent was met, and discovers the gap downstream. Make the shift a decision they can ratify, not a surprise.

## Close

- The hand-back reads, in order: original intent, evidence per item, scope shifts (if any), residual gaps (if any).
- The test of closure: the requester can confirm their intent was met in a single read, without re-deriving what they asked for.

## Boundaries

- This is a verification and communication procedure, not an implementation step — it runs at hand-back, over work already done.
- It does not relax any guard; a closed loop on incomplete work still reports the work as incomplete.
