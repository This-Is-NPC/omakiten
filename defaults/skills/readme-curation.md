---
name: README curation
description: Keeps install, usage, and examples in sync with the actual code surface.
schema_version: 2
role_affinity:
  - Scribe
---
The README is the entry point — the first and often only document a new user or contributor reads. Curation keeps its install steps, usage, and examples matching the real code surface, because a README that lies on the first command costs the reader's trust immediately.

## Keep it in sync with the code

Every command, flag, and example in the README is a claim about the current code. When the interface changes — a renamed flag, a new required argument, a moved entry point — the README changes in the same change. A stale install instruction or a broken example is a defect, not a documentation backlog item.

## Sections that earn their place

- **Install** — the exact, copy-pasteable steps that work from a clean state. Prefer one canonical path over a menu of half-tested options.
- **Usage** — the common invocations, shown as runnable commands with their real output where it helps.
- **Examples** — concrete, working scenarios that a reader can reproduce. An example that no longer runs is worse than no example.

Keep prerequisites to a one-line list with links rather than embedding tool-install tutorials; the README points to the standard tooling, it does not re-document it.

## Curation, not accretion

A README grows stale by accretion — sections added and never pruned. Curate actively: remove what no longer applies, keep it scannable, and resist turning it into the full manual. Deep reference belongs in dedicated docs the README links to.

## Boundaries

The README is the surface, not the knowledge base. Architecture, design rationale, and exhaustive reference live in their own documents; the README's job is to get a reader from zero to running and point them onward. Verify its claims against the code, the same standard as any other documentation.
