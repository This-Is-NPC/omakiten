---
name: Comment — Pre-mortem
description: Fills the `#pre-mortem` guard before implementation. Imagine the change has already failed.
entity: comment
laws:
  - template-fidelity
  - pre-mortem-required
---
**Imagine the change failed in production.** Write what went wrong.

**Failure modes**
- <mode 1> → detection: <signal> → mitigation: <plan>
- <mode 2> → detection: <signal> → mitigation: <plan>
- <mode 3> → detection: <signal> → mitigation: <plan>

**Blast radius** — <users / services / irreversibility class>

**Residual risk** — <what we accept because we can't mitigate further; reviewer agreement required>
