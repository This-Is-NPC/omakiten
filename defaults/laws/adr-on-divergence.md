---
name: ADR on divergence
severity: error
---
Architectural divergences (new external dependency, new bounded context, new persistence strategy, new transport, replacement of a load-bearing component) require an ADR in `docs/adr/<NNNN>-<title>.md` BEFORE implementation. Nygard format: Status · Context · Decision · Consequences · Alternatives considered.

Bad: swapped Postgres for DynamoDB across the order service with no ADR; six months later nobody remembers the trade-offs.
Good: ADR-0007 captures the swap and its consequences; the PR links to it.
