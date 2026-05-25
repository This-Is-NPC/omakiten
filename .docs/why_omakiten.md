# Why Omakiten

## What Omakiten Is

Omakiten is a CLI/TUI for managing tasks, context, and rules for AI-assisted software development.

It acts as a local source of truth where humans and AI agents can check the current state of a project before continuing work.

Short description:

> Opinionated checkpoints for AI-driven development.

## Why The Name

Omakiten combines two ideas:

- Omakase: an opinionated, curated experience where good defaults reduce ambiguity.
- Kiten: a starting point, origin, or reference point.

Together, Omakiten means an opinionated point of origin for continuing development safely.

## The Problem

AI agents can lose context, assume outdated state, or take actions outside the intended workflow.

Traditional task managers are not designed to be a safety layer for agentic development. They usually store tasks, but they do not enforce workflow rules, guardrails, or structured context handoff.

Omakiten is designed to solve that gap.

## Core Ideas

- Source of truth: tasks, dependencies, workflow state, and context are stored locally and consistently.
- Checkpoint: humans and agents can resume from a known state.
- Guardrails: invalid workflow actions are blocked with clear errors. Per-bucket CRUD policy and operation guards apply to delete/archive too, not only to transitions.
- Memory: a unified FTS5 `search` index covers tasks, comments, errors, solutions, and handoff context across every project on the machine — agents stop re-discovering the same fix.
- Speaks your language: 21 bundled CLI/TUI language packs; CLI and TUI share the install-time picker, agent-output language is chosen separately, and all three are switchable later (`okt config language set`).
- Observable by design: every meaningful state change emits a typed domain event; a YAML hooks engine fires async actions and notification cards; `metrics.summary` benchmarks agent behaviour per model over a chosen window.
- Token economy: agent-facing output is structured, compact, and predictable; context dumps are tiered (level 1–3) and capped at a token budget you set.
- Customization: workflows, laws, personas, skills, templates, themes, notifications, and language packs are shareable through YAML/Markdown — edit them, version them, copy a folder to a teammate.
- Local-first: every byte of state lives in a SQLite file under your home directory (or `.omakiten/` at the repo root). No account, no telemetry, no cloud.

## Positioning

Omakiten is not just a task manager.

It is a local checkpoint system for collaborative development with AI agents.

It is opinionated, but not closed. It provides strong defaults while keeping configuration portable and easy to share.

## Command Name

The suggested command is:

```bash
okt
```

Examples:

```bash
okt tui
okt list -b dev
okt context dump --level=2
okt move 42 --to done
okt mcp call search --input '{"query":"sqlite race","entity_types":["error","solution"]}'
okt mcp call metrics.summary --input '{"period":"30d"}'
```

## See also

- [Mental models](explanation/mental-models.md) — design principles in depth.
- [Workflow guide](workflow-guide.md) — concrete preset workflows.
