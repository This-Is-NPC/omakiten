---
name: Tracer debt acceptable
severity: info
---
Spike/tracer-bullet code is allowed to carry hardening debt — error handling stubs, missing edge-case coverage, hardcoded values. The reviewer flags the debt explicitly so the promotion path knows what to harden, but does not treat it as a blocker on the spike itself.

Bad: spike review demands production-grade error handling; spike never lands; the question it asked goes unanswered.
Good: "Tracer debt — `client.go:42` swallows the timeout error; acceptable for the spike. Block promotion to feature until handled."
