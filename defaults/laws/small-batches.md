---
name: Small batches
severity: warning
---
Prefer many small PRs (<400 LOC diff) over one large PR. Small batches review faster, revert cheaper, ship sooner, and shrink the blast radius of a regression. Optimizes the DORA lead-time and change-failure-rate metrics simultaneously.

Bad: one 2400-LOC PR that touches storage, API, and TUI; reviewer asks for changes; round-trip takes a week.
Good: same scope split into 6 sequential PRs with shared context comments; each lands in hours.
