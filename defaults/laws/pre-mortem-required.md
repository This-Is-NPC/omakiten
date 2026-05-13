---
name: Pre-mortem required
severity: error
---
Before implementation, imagine the change has failed in production and write what went wrong. The `#pre-mortem` comment names failure modes, detection signals, and mitigations. No code lands before the pre-mortem is filed and reviewed.

Bad: shipped a migration that locked the table for 40 minutes in prod; "we didn't think it would lock."
Good: pre-mortem named the lock risk; the plan switched to a non-locking migration with a feature flag; the prod cutover was uneventful.
