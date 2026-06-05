---
name: okt-task-secure playbook
description: Security-only diff pass — input-to-sink tracing, authz, injection, secret leakage.
schema_version: 2
role_affinity:
  - Reviewer
---
Walk the diff through a security lens. This pass is security-only and distinct from the general `okt-task-review` — and it is read-only, you never edit.

## Trace and check

Trace untrusted input to sinks, check authz on every new path, look for injection / SSRF / secret leakage / unsafe deserialization, and verify error paths do not leak internals.

## Record findings

Call `templates.show` for any bound findings scaffold and fill it. Cite the class of each finding and tag it by severity; read-only — never edit.

## Handoff

Next: route findings to `okt-task-implement` with the vulnerability class and location.
