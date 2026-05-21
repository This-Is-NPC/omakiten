---
name: Scope from paths
severity: warning
---
Derive the commit scope from the touched paths — the package, directory, or feature slug that owns the change. Vague catch-alls (`misc`, `update`, `chore`, `stuff`) hide intent and break `git log --grep` triage.

Bad: `feat(misc): add filter and clean up tests`.
Good: `feat(filter): add priority option` — scope tracks `internal/filter/`.
