---
name: Link task comments
severity: warning
---
When the change carries audit-trail comments on its Omakiten task — `#pre-mortem`, `#rollback-plan`, `#risk-assessment`, `#tests-passing`, `#peer-review` — reference them in the commit body as `Refs: task #<id> (#<tag>)`. Reviewers and future maintainers reading `git log` then have a single hop back to the rationale.

Bad: rollback plan filed on task #142, commit body says nothing — reviewer has to hunt.
Good: footer `Refs: task #142 (#rollback-plan, #pre-mortem)`.
