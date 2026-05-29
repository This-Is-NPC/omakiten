---
name: Implementation
description: Small coherent increments, tests for new and impacted behavior, regression analysis, bounded self-review.
schema_version: 2
role_affinity:
  - Builder
  - Tester
---
Implementation is the disciplined translation of an agreed requirement into working, tested code delivered in increments small enough to review and reason about. The unit of progress is a coherent change, not a finished feature dumped in one diff.

## Small coherent increments

Each increment does one thing and leaves the tree green. Prefer a sequence of reviewable steps over a single large diff: the smaller the step, the cheaper the review and the easier the revert. Keep refactoring commits separate from behaviour-changing commits so a reviewer can read intent.

## Tests for new and impacted behaviour

Cover the new behaviour with tests, and — critically — cover the behaviour the change *impacts*, not just the lines it adds. A change to a shared helper impacts every caller; the test surface follows the blast radius, not the diff size. On a bugfix, add the regression test that would have caught it.

## Regression analysis

Before declaring done, ask what existing behaviour this change could break. Trace the callers of every modified function and the consumers of every changed contract. Run the affected suites and read the diff once more for unintended behaviour, not just for correctness of the intended change.

## Bounded self-review

Read your own diff as a reviewer would before handing it off: naming, dead code, leftover debugging, missing error handling, and whether the tests actually assert the behaviour rather than merely exercising it. Self-review is bounded — it catches the obvious, not everything; it does not replace an independent review.

## Boundaries

Implementation realises an agreed design; it does not redefine scope. When the work reveals the requirement was wrong, surface it rather than silently expanding the change. Cite refactoring and smell catalogs by name when the increment includes structural cleanup.
