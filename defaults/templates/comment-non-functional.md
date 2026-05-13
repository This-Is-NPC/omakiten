---
name: Comment Non-functional
description: NFRs separated from functional — performance / security / usability / observability / scale / accessibility / compliance.
entity: comment
laws:
  - template-fidelity
  - non-functional-explicit
---
| Attribute | Target | Notes / rationale |
| --- | --- | --- |
| Performance | <p50 / p95 / p99 latency; throughput; resource ceiling> | |
| Security | <auth model; data sensitivity class; threat model link> | |
| Usability | <accessibility level (WCAG); supported languages; UX flows touched> | |
| Observability | <metrics / logs / traces emitted; dashboards updated> | |
| Scale | <expected load; saturation point; degradation behaviour> | |
| Accessibility | <keyboard / screen-reader / contrast / motion expectations> | |
| Compliance | <regulatory frame: GDPR / SOC2 / HIPAA / LGPD / none> | |

Each row carries either a concrete target OR "not applicable + reason". Empty cells block the move forward — explicit "not applicable" is the safety valve.
