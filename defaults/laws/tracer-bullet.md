---
name: Tracer bullet
severity: warning
---
Ship a walking-skeleton end-to-end before adding depth to any single layer. Connect the wires first; flesh out logic only after the whole shape is observable. Half a feature is worse than a thin slice of the whole.

Bad: spent the spike polishing the storage layer with no UI/API path wired up; nothing demoable at the end.
Good: thin HTTP-handler → service → in-memory store, all returning canned data, then iterated on the parts that mattered.
