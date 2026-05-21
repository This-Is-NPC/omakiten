---
name: Pre-mortem aware
severity: warning
---
For changes that filed a `#pre-mortem`, the reviewer reads it before walking the diff. Findings reference the recorded failure modes — confirms whether mitigations actually shipped, flags failure modes the diff missed.

Bad: pre-mortem named "DB connection pool exhaustion" as top risk; reviewer ignored it; PR merges without the pool cap.
Good: pre-mortem read first; review finding `db.go:42`: "pre-mortem #2 (pool exhaustion) — diff sets `MaxOpenConns` but no `MaxIdleConns` cap; mitigation is incomplete."
