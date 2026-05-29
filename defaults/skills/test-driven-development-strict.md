---
name: Test-driven development (strict)
description: Red → green → refactor with coverage-gate awareness; tests-first + coverage delta + perf regression check.
schema_version: 2
role_affinity:
  - Builder
  - Tester
---
The strict variant of test-driven development keeps Beck's red → green → refactor cycle (Beck, 2002) but adds two non-negotiable gates that must hold before the work is considered done: a coverage delta and a performance regression check. Use it on critical paths and regulated or high-reliability code where "the tests pass" is not a sufficient bar.

## The strict cycle

Run the standard loop — write a failing test, pass it with the minimum change, refactor green — but never leave the cycle with a coverage regression or an unexplained performance change. Each gate is checked per increment, not deferred to the end.

## Coverage delta gate

After each green increment, compare coverage against the baseline. New behaviour carries line and branch coverage at or above the project floor; changed packages do not regress. Where a line genuinely cannot be tested (defensive guard, OS-error path), mark it with a justified-gap annotation and a one-line rationale rather than letting coverage drop silently. Carry the per-package delta into the check report.

## Performance regression check

For paths with a stated performance budget, run the relevant benchmark before and after the change and compare against the budget, not against intuition. A change that passes correctness but regresses latency or allocation past the budget is not done. Record the before/after numbers so a reviewer can see the headroom.

## Boundaries

The strict variant raises the bar on the same cycle; it does not replace integration or exploratory testing. Reserve it for code where the coverage and performance gates earn their cost — applying it everywhere taxes velocity without proportional risk reduction.
