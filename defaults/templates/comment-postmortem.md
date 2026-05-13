---
name: Comment — Postmortem
description: Blameless incident or near-miss writeup. Mirrors `docs/postmortems/<YYYY-MM-DD>-<title>.md`.
entity: comment
laws:
  - template-fidelity
  - blameless-postmortem
---
**Incident summary** — <one paragraph; what users / systems experienced>

**Timeline** (UTC)
- HH:MM — <event>
- HH:MM — <event>

**Detection latency** — <time from incident start to first signal>

**Customer impact** — <users affected, surfaces affected, duration>

**Root cause** — 5-whys; never "human error":
1. Why? <observed failure>
2. Why? …
5. Why? <system gap>

**Action items**
- [ ] <action> — owner: <name> — due: <date>

**Postmortem file** — `docs/postmortems/<YYYY-MM-DD>-<title>.md`
