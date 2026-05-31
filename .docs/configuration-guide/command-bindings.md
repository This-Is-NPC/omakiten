# Command bindings

`mcp_commands:` binds stable `okt-*` command names to the runtime entities that render into an MCP prompt. Command names, tiers, roles, scopes, and write behavior are documented in [../command-surface.md](../command-surface.md). This page owns the configurable binding schema.

Source files:

- `internal/config/bundle.go` (`MCPCommandSpec`, `PersonaWiring`).
- `internal/config/validator.go::validateMCPCommands` and `validateMCPCommandSkillSubset`.
- `internal/agent/service_command.go` (prompt rendering).

## Contents

- [`mcp_commands`](#mcp_commands)
- [Prompt composition](#prompt-composition)
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

1. Command action text comes from `internal/agent/command_table.go`.
2. `mcp_commands.<command>.persona` selects the persona body.
3. `mcp_commands.<command>.skills` selects the skill subset to render.
4. Effective laws are `global laws + persona laws + command laws + template laws - laws_disabled`.
5. Templates are listed as metadata; commands that need a body call `templates.show`.

The MCP protocol anatomy, caching, and message flow are documented in [../mcp.md#anatomy-of-an-mcp-command](../mcp.md#anatomy-of-an-mcp-command).

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

Missing command names are allowed. A command without explicit binding still resolves action text, but it has no persona-specific body unless the active profile binds one.

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
- Skill subset validation changes.
- The command surface adds/removes/renames a command and bindings need a new example.
