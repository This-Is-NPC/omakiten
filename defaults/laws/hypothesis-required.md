---
name: Hypothesis required
severity: error
---
Every spike answers a written question. The hypothesis lives on the task body or in a `#hypothesis` comment before any code is written. If the question can't be stated in one sentence with a falsifiable signal, the spike isn't ready — it is wandering.

Bad: opened a spike titled "explore caching options" with no acceptance signal.
Good: "Hypothesis: an LRU cache on `taskByID` cuts p95 dump latency below 50ms. Signal: dump benchmark p95 < 50ms on the canonical fixture."
