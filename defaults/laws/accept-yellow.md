---
name: Accept yellow
severity: info
---
Spike/tracer-bullet checks tolerate yellow signal — non-critical lint warnings, soft coverage drops, deferred hardening. The check runner reports the yellow target explicitly so the promotion path knows what to harden, but does not block the spike on it.

Bad: spike check fails on a `golint` style warning; spike never lands; the question it asked goes unanswered.
Good: "`lint: yellow — 3 style warnings deferred`; block promotion to feature until cleared."
