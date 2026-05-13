---
name: Error-budget aware
severity: warning
---
Reliability-affecting changes cite the current error-budget consumption before shipping. If the budget is exhausted, only fixes and rollbacks ship — features wait. Reference the SLO definition the change touches; do not invent budgets per task.

Bad: shipped a latency-affecting feature against an exhausted budget because "the calendar said this sprint."
Good: `#risk-assessment` quotes the SLO and the current budget burn; the feature is deferred until the budget refills, with the dependency tracked.
