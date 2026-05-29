---
name: Documentation
description: Generates and reviews architecture, requirements, and contributor docs; claims traceable to code.
schema_version: 2
role_affinity:
  - Scribe
  - Reviewer
---
Documentation captures what the system is and how to work on it, in a form a reader can trust. The governing rule is traceability: every claim a document makes about the code is verifiable against the code, so the docs do not quietly drift into fiction.

## What to produce

- **Architecture docs** — the tech stack, dependencies, design patterns, infrastructure, and code metrics, each tied to where it lives in the tree. The reader should be able to open the cited file and confirm the claim.
- **Requirements docs** — the functional, non-functional, and business-rule set, mapped to the code that implements each, so the document doubles as a coverage index.
- **Contributor docs** — how to build, test, and contribute, kept in sync with the actual commands and conventions in the repo rather than an idealised version.

## Traceability discipline

A documentation claim without a source reference is an assertion, not a fact. Cite the file (and where useful the symbol) behind each non-trivial statement. When the code and the doc disagree, the code wins and the doc is wrong — fix it in the same change that revealed the gap.

## Review, not just generation

Documentation is reviewed like code. Check it for stale claims, broken references, and statements the code no longer supports. A generated draft is a starting point; the review is where it becomes trustworthy.

## Boundaries

This skill covers the durable knowledge base (architecture, requirements, contributor docs). It is distinct from design-documentation (the pre-build approach record), decision-records (the why behind a choice), and README curation (the entry-point surface). Route each document to the form that owns it rather than overloading one file.
