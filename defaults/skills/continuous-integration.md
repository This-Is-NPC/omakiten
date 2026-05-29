---
name: Continuous integration
description: Pre-push verification, CI as source of truth for green, fix-forward vs revert decision discipline.
schema_version: 2
role_affinity:
  - Builder
  - Committer
  - Tester
---
Continuous integration (Fowler, *Continuous Integration*, 2006; practice popularised from Beck's Extreme Programming) means every change is integrated into the shared mainline and verified by an automated build many times a day. The point is to find integration problems within minutes of introducing them, while they are still small.

## Pre-push verification

Run the build and the relevant test suite locally before pushing. The local gate catches the obvious break before it consumes CI capacity and before it blocks teammates. It does not replace CI — it shortens the feedback loop and keeps the shared pipeline mostly green.

## CI as the source of truth

"Green" means green on CI, not green on a developer's machine. The CI environment is the canonical build; local passes are necessary but not sufficient. Treat a red mainline as a stop-the-line event: the team's first priority is restoring green, because everyone builds on top of the mainline.

## Fix-forward vs revert

When the mainline goes red, choose deliberately:

- **Revert** when the fix is not immediately obvious or the break is blocking others. Reverting restores green fast and lets the change be reworked off the critical path. This is the default under doubt.
- **Fix-forward** only when the fix is small, well-understood, and faster than a revert-and-retry. A speculative fix-forward that might fail again keeps the line red longer than a clean revert.

## Boundaries

CI verifies integration continuously; it is only as trustworthy as its test suite. Pair it with coverage and static-analysis gates so "green" means "verified," not merely "compiled." It underpins trunk-based development — without a fast reliable CI gate, continuous integration to trunk is unsafe.
