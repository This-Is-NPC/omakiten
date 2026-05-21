---
name: Requirements coverage check
severity: warning
---
Coverage is measured against the recorded acceptance criteria, not just lines of code. Each AC is either exercised by an automated test, exercised by a documented manual check, or explicitly waived in a decision record — no fourth option. Line coverage that ignores AC traceability ships features that "pass" without anyone confirming they meet the requirement.

Bad: 90% line coverage; AC §3 ("rate-limit at 5 req/s") never exercised; production blows past the limit on day one.
Good: `#check-report` maps each AC to the test that exercises it, or names the decision record waiving it.
