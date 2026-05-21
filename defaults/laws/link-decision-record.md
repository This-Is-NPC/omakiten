---
name: Link decision record
severity: warning
---
When the change implements or diverges from a decision recorded in the repo (decision record, RFC, design doc), link it from the commit body as `Refs: <path>` (e.g. `Refs: docs/decisions/0042-replace-sqlite-driver.md`). The commit becomes the bridge between the recorded rationale and the diff that realises it.

Bad: swapped the SQLite driver per ADR 0042; commit body never names the ADR.
Good: footer `Refs: docs/decisions/0042-replace-sqlite-driver.md`.
