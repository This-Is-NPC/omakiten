---
name: Pull Request
description: PR scaffold — before/after, changes, files, validation, deviations, risks, references.
entity: pr
default: pr
laws:
  - template-fidelity
---
## Before
- {pain point}

## After
- {outcome}

## Summary of changes
| Aspect | Change |
| --- | --- |
| {aspect} | {change} |

## Files updated
- `{path}`

## Validation
| Scenario | Outcome |
| --- | --- |
| {scenario} | {outcome} |

## Deviations
| Criterion | Deviation | Rationale |
| --- | --- | --- |
| {criterion} | {diverged} | {why} |

## Risks / follow-ups
- {risk}

## References
- Commits: `{hash}`
- Okt task: #{id}
- Branch: `{name}`
