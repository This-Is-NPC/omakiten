---
name: Design before code
severity: error
---
Implementation starts only after the bounded-context map and aggregate boundaries for the change are documented. Code follows the model, not the other way around. Sketch the ports the application needs from the outside before picking adapters.

Bad: opened a SQL editor and started shaping tables; the resulting model leaks storage concerns into the domain.
Good: wrote `bounded-context-map.md` first; identified aggregates and ports; chose Postgres only after the model held up.
