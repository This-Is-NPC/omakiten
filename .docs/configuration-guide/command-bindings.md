# Command bindings

`mcp_commands:` binds stable `okt-*` command names to the runtime entities that render into an MCP prompt. Command names, tiers, roles, scopes, and write behavior are documented in [../command-surface.md](../command-surface.md). This page owns the configurable binding schema.

Source files:

- `internal/config/bundle.go` (`MCPCommandSpec`, `PersonaWiring`).
- `internal/config/validator.go::validateMCPCommands` and `validateMCPCommandSkillSubset`.
- `internal/agent/service_command.go` (prompt rendering).

## Contents

- [`mcp_commands`](#mcp_commands)
- [Prompt composition](#prompt-composition)
- [Playbook skills](#playbook-skills)
- [Persona skill repertoires](#persona-skill-repertoires)
- [Template-bound laws](#template-bound-laws)
- [Validation](#validation)
- [Importing command bindings](#importing-command-bindings)
- [Update when](#update-when)

## `mcp_commands`

```yaml
mcp_commands:
  global:
    laws: [template-fidelity, authorize-remote-writes]
  okt-task-create:
    persona: owner
    skills: [user-story-writing, invest-stories]
    laws: [outcome-over-output]
    templates: [user-story]
  okt-task-imagine:
    persona: owner
    skills: [discovery]
    laws_disabled: [template-fidelity]
```

| Field | Type | Notes |
|---|---|---|
| `persona` | slug | Persona body rendered under `## Persona`. Ignored on `global`. |
| `skills` | list of slugs | Command-specific subset of the persona's `skill_repertoire`. |
| `laws` | list of slugs | Added to the effective law set. On `global`, inherited by every command. |
| `laws_disabled` | list of slugs | Removed after global/persona/command/template laws are unioned. |
| `templates` | list of slugs | Template metadata rendered in the prompt; bodies are fetched just in time. |

`global` is reserved. It supplies inherited laws only; persona, skills, and templates under `global` are ignored.

## Prompt composition

Every `okt-*` prompt resolves to one markdown `PromptMessage`:

1. The command playbook is **entity-sourced**: the command binds an `okt-<slug>-playbook` skill via `mcp_commands.<command>.skills`, and the bound playbook skill's body renders inside the `## Skills` section like any other skill body. There is no `## Action` section and no hardcoded Go prose — `internal/agent/command_table.go` is a bare slug table with no Action/Description strings.
2. The prompts/list one-liner for the command is the bound playbook skill's frontmatter `description`. `internal/agent/service_command.go` reads it from the live skill catalog; an unwired runtime (no playbook skill bound) degrades to an empty description rather than falling back to Go.
3. `mcp_commands.<command>.persona` selects the persona body, rendered under `## Persona`.
4. `mcp_commands.<command>.skills` selects the skill subset to render under `## Skills` — the bound `okt-<slug>-playbook` skill is one of those slugs, listed alongside the persona's other capability skills.
5. Effective laws are `global laws + persona laws + command laws + template laws - laws_disabled`, rendered under `## Laws`.
6. Templates are listed as metadata under `## Templates`; commands that need a body call `templates.show`.

The MCP protocol anatomy, caching, and message flow are documented in [../mcp.md#anatomy-of-an-mcp-command](../mcp.md#anatomy-of-an-mcp-command).

## Playbook skills

The operational playbook for every command lives in a command-named `okt-<slug>-playbook` skill under `defaults/skills/` — `okt-task-continue` binds `okt-task-continue-playbook`, `okt-shape` binds `okt-shape-playbook`, and so on. The mapping is the deterministic naming convention `playbookSlugForCommand` applies (`<command>-playbook`); the resolver looks that slug up in the live skill catalog and renders its body as the command's playbook, its frontmatter `description` as the prompts/list one-liner.

```markdown
---
name: okt-task-continue playbook
description: Read a task's checkpoint before resuming work.
schema_version: 2
---
Read a task's checkpoint — understand where the task stopped, do not start coding…
```

The bare `okt` shortcut and the explicit `okt-start` both resolve their playbook to `okt-start-playbook`, so the shortcut renders the same smart-entry body.

These playbook skills are a deliberate exception to the [generic-skills norm](entities.md#skills): a skill is normally a reusable capability any persona may pick up, but an `okt-<slug>-playbook` skill is **command-locked** — it carries the operational prose for exactly one command and is meaningful only when bound to that command. Bind a playbook skill into the matching command's `skills:` list (and the persona's `skill_repertoire`, since command `skills:` must be a subset); do not reuse it elsewhere.

## Persona skill repertoires

Persona wiring declares the full skill pool a persona may use:

```yaml
personas:
  - slug: builder
    schema_version: 2
    skill_repertoire:
      - implementation
      - test-driven-development
      - markdown
```

Command `skills:` must be a subset of the bound persona's `skill_repertoire`. This keeps prompts small and prevents a command from pulling a capability the persona does not declare.

Legacy persona `skills:` is migrated to `skill_repertoire` by the schema-v2 migrator. New config should use `schema_version: 2` and `skill_repertoire` directly.

## Template-bound laws

Template frontmatter may declare laws that apply whenever the template is bound to a command:

```markdown
---
name: Pull Request
default: pr
laws:
  - template-fidelity
---
```

Persona frontmatter laws merge with persona wiring laws. Template frontmatter laws have no YAML wiring counterpart; they travel with the template asset.

## Validation

| Rule | Error or warning shape |
|---|---|
| Duplicate slug in `laws`, `laws_disabled`, `templates`, or `skills` | validation error naming `mcp_commands.<name>.<field>` |
| Same law appears in `laws` and `laws_disabled` on one command | validation error |
| Command skill not in persona `skill_repertoire` | validation error from `validateMCPCommandSkillSubset` |
| Missing persona/law/template reference | source warning so partially-authored packs can still load visibly |
| `mcp_commands` in a sub-task kit | warning; sub-kits ignore MCP command bindings because MCP resolves at project root |

Missing command names are allowed. A command without an explicit binding still registers, but it has no persona body, no skill subset, and — without a bound `okt-<slug>-playbook` skill — an empty playbook and prompts/list description until the active profile binds one.

## Importing command bindings

Command bindings can be split from the active profile:

```yaml
mcp_commands:
  from: ./packs/commands/strict-command-bindings.yaml
```

The import replaces the `mcp_commands:` map wholesale. Full import rules live in [path-resolution.md § Modular config imports](path-resolution.md#modular-imports).

## Update when

- `MCPCommandSpec` gains or loses a field.
- Prompt composition order changes in `internal/agent/service_command.go`.
- The playbook-skill naming convention (`playbookSlugForCommand`) or its source-of-truth role changes.
- Skill subset validation changes.
- The command surface adds/removes/renames a command and bindings need a new example.
