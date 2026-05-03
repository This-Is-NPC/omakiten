# Architecture

## Overview

Omakiten is a local-first CLI/TUI for managing tasks, workflow rules, guardrails, and handoff context for AI-assisted development.

The system is designed around one core rule: CLI and TUI must not implement business rules directly. Both interfaces call application services, and those services enforce project scope, workflow transitions, and persistence rules.

## Storage Model

Omakiten uses two storage layers:

- Global SQLite database for operational state.
- Canonical YAML files for shareable configuration.

The global database lives at:

```txt
~/.local/share/omakiten/omakiten.db
```

The main configuration file lives at:

```txt
~/.config/omakiten/omakiten.yaml
```

Themes are separate:

```txt
~/.config/omakiten/themes/*.yaml
```

## Canonical Configuration

`omakiten.yaml` is the source of truth for:

- config
- laws
- workflows
- personas
- skills

SQLite stores a materialized copy of this data for fast reads, stable IDs, and runtime queries. Any command or TUI action that changes laws, workflows, personas, skills, or config must update `omakiten.yaml` first, validate it, and then reimport it into SQLite.

## Project Isolation

The database is global, so project isolation is mandatory.

Operational data must always be scoped by `project_id`, including:

- tasks
- comments
- dependencies
- context entries

Commands resolve the active project by this order:

- explicit `--project-id`
- explicit `--project` slug
- current working directory inside a registered project root

Queries that return or mutate tasks must always filter by `project_id`.

## Layers

```txt
cmd/okt          binary entrypoint
internal/cli     CLI commands and JSON output wiring
internal/tui     terminal UI models and views
internal/app     application services and use cases
internal/domain  core entities and coded errors
internal/config  YAML contracts, validation, and canonical writes
internal/sqlite  global database, migrations, and repositories
internal/project project resolution
internal/output  minified JSON envelopes
internal/token   token counting abstractions
internal/graph   task dependency helpers
```

## Security Rules

- All writes go through `internal/app` services.
- SQLite uses foreign keys and transactions for writes.
- Workflow transitions are enforced centrally.
- Task dependencies cannot cross projects.
- YAML loading uses strict field validation.
- CLI responses use stable JSON error codes for agents.
- `context dump` must never mix data from different projects.

## Testing Strategy

Table-driven tests are preferred for business rules and validation.

Priority coverage:

- YAML validation
- workflow transitions
- project resolution
- project-scoped task queries
- context dump levels
- minified JSON output
- dependency graph rules
