---
name: Blast-radius awareness
severity: warning
---
Every change declares its blast radius: users affected, services touched, irreversibility class. The classification drives gate severity — a critical-radius change demands stricter sign-off than a contained one. Default to overestimating; reviewers can downgrade.

Bad: a "small fix" turned out to clear the cache for every customer at peak hour; nobody flagged the radius.
Good: `#risk-assessment` rates the radius `high (all tenants, partial irreversibility on cache warm-up)`; reviewers required a feature flag and a staged rollout.
