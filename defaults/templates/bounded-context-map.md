---
name: Bounded context map
description: Discovery doc — context name, ubiquitous language, integrations, anti-corruption layers.
entity: discovery
laws:
  - template-fidelity
  - ubiquitous-language
---
# <Context name>

## Purpose
<what part of the business this context owns; one paragraph>

## Ubiquitous language
| Term | Meaning in this context |
| --- | --- |
| <Term> | <definition the domain expert would sign off on> |

## Aggregates
- **<AggregateName>** — invariants enforced; commands that mutate it; events emitted

## Ports (application boundary)
- **<PortName>** — purpose; adapters that satisfy it

## External integrations
- **<Other context>** — relationship (customer / supplier / conformist / partner / shared kernel); anti-corruption layer if any

## Open questions
<what is still unclear; what the domain expert needs to confirm>
