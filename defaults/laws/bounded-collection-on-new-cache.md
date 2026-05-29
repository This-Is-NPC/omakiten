---
name: Bounded collection on new cache
severity: error
---
Any new cache, in-memory map, or accumulating collection ships with an explicit bound: a size cap, a time-to-live, or an eviction policy. An unbounded collection that grows with traffic or uptime is a latent memory leak — it works in every test and fails in production after days of load. State the bound and its rationale when the collection is introduced; "it won't grow much" is a prediction, not a bound.

Bad: a per-request response cache keyed by full URL, no eviction; under crawler traffic the process OOM-killed nightly until someone added an LRU.
Good: the same cache declared as an LRU with a 10k-entry cap and a 5-minute TTL, sized from the observed working set, with a metric on eviction rate.

See Nygard, *Release It!* (2nd ed.), "Unbounded Result Sets" and "Slow Responses" stability antipatterns.
