---
name: Severity tagged
severity: warning
---
Every finding carries an explicit severity — `error` (bug, security, data corruption, broken contract), `warning` (smell, fragile assumption, latent risk), `info` (suggestion, drive-by). The author needs the tag to triage; reviewers without it force the author to re-read every line.

Bad: "the loop allocates inside the hot path".
Good: "warning: the loop allocates inside the hot path — `handler.go:84` — pre-size the slice or move the allocation out."
