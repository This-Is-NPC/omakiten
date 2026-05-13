---
name: Aggregate consistency
severity: warning
---
Inside an aggregate boundary, invariants are enforced synchronously in the same transaction. Across boundaries, eventual consistency only — via domain events, sagas, or process managers. Never reach into a sibling aggregate's internals to fix consistency "just this once".

Bad: order processing reaches into the inventory aggregate to decrement stock in the same transaction; both aggregates now share a hidden lock.
Good: order emits `OrderConfirmed`; inventory reacts via a handler; consistency is observable through the event.
