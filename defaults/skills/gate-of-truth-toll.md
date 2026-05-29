---
name: Gate of truth toll
description: Strict test-first discipline — red → green → refactor with a coverage-delta gate and a performance regression check; every passage is paid for in evidence.
schema_version: 2
role_affinity:
  - Builder
  - Tester
---
The gate of truth toll is the strict variant of test-driven development: you cross into "done" only by paying the toll in evidence. It keeps Beck's red → green → refactor cycle (Beck, 2002, "Test-Driven Development: By Example") but adds two non-negotiable gates — a coverage delta and a performance regression check — that must hold before the work is accepted. Reserve it for critical paths and high-reliability code where "the tests pass" is not a sufficient bar.

## The strict cycle

Write a failing test, pass it with the minimum change, refactor while green — but never leave the cycle with a coverage regression or an unexplained performance shift. Each gate is checked per increment, not deferred to the end.

## Coverage-delta gate

After each green increment, compare coverage against the baseline. New behaviour carries line and branch coverage at or above the project floor; changed packages do not regress. Where a line genuinely cannot be tested, mark it with a justified-gap annotation and a one-line rationale rather than letting coverage drop silently. Carry the per-package delta into the report.

## Performance regression check

For paths with a stated performance budget, run the relevant benchmark before and after and compare against the budget, not against intuition. A change that passes correctness but regresses latency or allocation past its budget has not paid the full toll. Record the before/after numbers so a reviewer sees the headroom.

## Boundaries

The strict variant raises the bar on the same cycle; it does not replace integration or exploratory testing. Apply it where the coverage and performance gates earn their cost — using it everywhere taxes velocity without proportional risk reduction.
