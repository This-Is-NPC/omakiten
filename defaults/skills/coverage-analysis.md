---
name: Coverage analysis
description: "Coverage thresholds — line vs branch — and the justified-gap pattern for accepting documented gaps."
---
Coverage discipline (Ned Batchelder, coverage.py 2004; SQLite TH3):

- **Line coverage** — every executable line is run by at least one test. Cheap to measure, easy to game (running ≠ asserting). Threshold floor: ≥80% line on new behaviour.
- **Branch coverage** — every conditional outcome (`if`/`else`, `case`, short-circuit) is exercised. Catches dead branches line coverage misses. Threshold floor: ≥70% branch on changed packages when the tooling reports it.
- **Mutation coverage** — perturb the code, expect tests to fail. Highest-fidelity signal; expensive to run. Reserve for critical paths.

Justified-gap pattern: when a line or branch cannot be reasonably tested (unreachable defensive guard, panic-on-OS-error path), drop a `// coverage:ignore` (or the equivalent for the language) with a one-line rationale on the same line. The gap is then documented, intentional, and visible in `git blame` — not silent.

Report shape (carry into `#tests-passing` / `#check-report`):
- `package | line% (Δ) | branch% (Δ) | uncovered hot spots`
- Uncovered hot spots = files where coverage dropped > 1.0 point. Name them; do not generalize.
