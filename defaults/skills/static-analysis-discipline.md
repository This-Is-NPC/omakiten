---
name: Static analysis discipline
description: Lint / security (SAST) / coverage / SCA gates as part of Definition of Done; no merge with new warnings.
schema_version: 2
role_affinity:
  - Reviewer
  - Tester
  - Committer
---
Static analysis discipline treats automated code analysis as a Definition-of-Done gate, not an advisory. Four gates run before merge, and none may regress: lint, security (SAST), coverage, and software-composition analysis (SCA). The rule is simple and enforced — no merge introduces a new warning.

## The four gates

- **Lint** — style and correctness rules the language tooling enforces (vet, golangci-lint, eslint, and the like). Catches the class of bug that is cheap to find mechanically and expensive to find in review.
- **Security / SAST** — static application security testing flags injection, unsafe deserialisation, weak crypto, and tainted-data flows from the source. Pairs with a manual security-review lens for the findings tools cannot infer.
- **Coverage** — line and branch coverage against the project floor on new and changed code; a coverage regression is a gate failure, with justified gaps annotated explicitly.
- **SCA** — software-composition analysis scans dependencies (and transitive ones) for known CVEs and license issues. A pinned dependency with a known vulnerability fails the gate.

## No new warnings

The discipline holds the line on the *delta*: existing debt may be tracked separately, but a change must not add a new warning to any gate. This keeps the warning count from ratcheting upward, where it eventually becomes noise everyone ignores. Suppressions are explicit, annotated, and reviewable — never blanket.

## Boundaries

Static analysis finds the mechanically-detectable; it does not judge design, correctness of intent, or behaviour under load. It complements human review and dynamic testing rather than replacing them. Wire the gates into CI so the bar is enforced uniformly, not left to individual diligence.
