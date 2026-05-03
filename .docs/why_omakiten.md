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
- Guardrails: invalid workflow actions are blocked with clear errors.
- Token economy: agent-facing output is structured, compact, and predictable.
- Customization: workflows, laws, personas, skills, and config are shareable through YAML.

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
```
