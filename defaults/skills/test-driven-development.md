---
name: Test-driven development
description: Red → green → refactor; tests-first for new behavior; regression test on every bugfix.
schema_version: 2
role_affinity:
  - Builder
  - Tester
---
Test-driven development (Beck, *Test-Driven Development: By Example*, 2002) drives design and implementation from a failing test. The cycle is short and strict: write a test that fails, make it pass with the simplest change, then refactor under the safety of the green bar.

## Red → green → refactor

- **Red** — write one small test for behaviour that does not yet exist and watch it fail. A test that passes before you write the code is testing nothing; the red step proves the test can fail.
- **Green** — write the minimum production code to pass the test, even if it is ugly. Do not add behaviour the current test does not demand.
- **Refactor** — with the test green, clean up the implementation and the test. The green bar is the licence to refactor safely; run the suite after each structural change.

Keep the loop tight — minutes, not hours. Long red phases usually mean the test is too large; split it.

## Tests-first for new behaviour

New behaviour starts with a failing test that pins the intended outcome. This keeps the design honest (you write the code that is needed) and leaves a regression net behind by construction.

## Regression test on every bugfix

A bug is a missing test. Before fixing, write the test that reproduces the failure and watch it fail; then fix until it passes. The bug can now never recur silently — the test guards it.

## Boundaries

TDD drives unit-level design; it is not a substitute for integration, contract, or exploratory testing. When coverage thresholds and performance regression gates also apply, escalate to the strict variant.
