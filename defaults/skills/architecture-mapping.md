---
name: Architecture mapping
description: Tech stack, dependencies, design patterns, infrastructure, code metrics with measurable references.
schema_version: 2
role_affinity:
  - Scribe
  - Reviewer
---
Architecture mapping reconstructs the system's structure from the code and configuration that define it, producing a referenced overview a newcomer can orient by. Every claim points to where it lives, so the map is verifiable rather than an impression.

## What to map

- **Tech stack** — languages, frameworks, runtimes, and datastores in actual use, identified from manifests and build files rather than assumed.
- **Dependencies** — direct and significant transitive dependencies, their roles, and any with known risk. Cite the manifest.
- **Design patterns** — the recurring structural choices (layering, dispatch, dependency injection, the module boundaries) with a representative file for each so the reader can see the pattern instantiated.
- **Infrastructure** — how the system is built, deployed, and run: CI configuration, container and deployment manifests, environment boundaries.
- **Code metrics** — size and shape signals (module count, hotspots, coupling indicators) reported as measured numbers, not adjectives.

## Measurable references

Anchor each statement to a source: a file path, a config key, a metric value. "The service uses dependency injection" is an impression; "the composition root in `cmd/main` wires concretions into the interfaces in `internal/service`" is a map. A reader should be able to follow any claim to the artifact behind it.

## Method

Work outside-in: start from entry points and configuration, follow the wiring inward, and record the structure as you go. Group by concern. Mark inferred or uncertain conclusions as such rather than presenting them with false confidence.

## Boundaries

The map describes the current structure; it does not judge whether the architecture is good or prescribe changes — that is a review or design task. It feeds the durable architecture documentation and gives reviewers the structural baseline to assess a change against.
