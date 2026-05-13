---
name: Blameless postmortem
severity: error
---
Any production incident or near-miss earns a `#postmortem` comment AND a `docs/postmortems/<YYYY-MM-DD>-<title>.md` file: timeline (UTC), detection latency, customer impact, 5-whys root cause, action items with owners and due dates. "Human error" is never a root cause — it is the system that allowed the error.

Bad: root cause = "Alice forgot to run the migration." No action items.
Good: root cause traces to a missing migration-runner step in CI; action items add the gate.
