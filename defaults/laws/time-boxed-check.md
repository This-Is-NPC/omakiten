---
name: Time-boxed check
severity: warning
---
Spike presets cap check effort up front — declare the box (e.g. 5 minutes) before running. When the box expires, ship the partial report with the time used noted; do not blow the box hunting marginal targets on throwaway code.

Bad: 30 minutes spent retrying flaky integration targets on a 2-hour spike; the spike is colder than the report by the time it lands.
Good: "Box: 5 min. Used: 4 min. Test + critical-lint only; integration suite deferred."
