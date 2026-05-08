---
name: No silent behavior changes
severity: error
---
Every behavioral change ships with explicit evidence: a failing-then-passing test, a `#resume` comment naming the change, or a commit message calling it out. Incidental behavior shifts inside a refactor are still behavior changes — document them.
