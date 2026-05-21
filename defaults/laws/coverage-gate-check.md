---
name: Coverage gate check
severity: warning
---
Reviewer verifies that the change keeps coverage at or above the project threshold — line and branch when both are tracked. Coverage drop without a written justification is a finding, not a footnote.

Bad: "tests look fine" — reviewer never checked coverage; merge drops line coverage 3 points silently.
Good: "coverage line +0.2 / branch +0.1 — confirmed against the threshold; no regression."
