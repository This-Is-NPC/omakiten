# Enums and template defaults

This page owns configurable id-to-label tables and template default slots under `config:`. These settings are data, not command or workflow semantics.

Source files:

- `internal/config/bundle.go` (`PriorityDefinition`, `SeverityDefinition`).
- `internal/config/validator.go` enum validation.
- `internal/domain/registry.go`.

## Contents

- [`config.template_defaults`](#configtemplate_defaults)
- [`config.priorities`](#configpriorities)
- [`config.severities`](#configseverities)
- [Runtime wiring](#runtime-wiring)
- [Update when](#update-when)

## `config.template_defaults`

```yaml
config:
  template_defaults: [task, pr, comment-resume, comment-selfbranch, comment-documentation]
```

Each template may claim one `default:` kind in its frontmatter. Runtime rules:

- A global template can claim a default kind.
- A project-scoped template can claim the same kind for one project.
- Project-scoped defaults win inside that project.
- At most one template may claim a `(default, project)` pair.

Template frontmatter is documented in [entities.md#templates](entities.md#templates).

## `config.priorities`

```yaml
config:
  priorities:
    - id: 1
      value: low
      color: success
    - id: 2
      value: normal
      default: true
      color: info
    - id: 3
      value: high
      color: error
```

| Field | Type | Notes |
|---|---|---|
| `id` | int `> 0` | Storage handle and sort weight. Declare low to high. |
| `value` | string | Human label in TUI badges, CLI output, and MCP DTOs. |
| `default` | bool | At most one entry. Used when a task is created without explicit priority. |
| `color` | string, optional | Theme token for badges: `error`, `warning`, `success`, or `info`. |

Validation rejects non-positive ids, duplicate ids, empty values, duplicate values, descending ids, and more than one default.

Renaming `value` is safe: persisted tasks keep their integer `priority_id`, and labels are projected at read time.

## `config.severities`

```yaml
config:
  severities:
    - id: 1
      value: info
      color: info
    - id: 2
      value: warning
      default: true
      color: warning
    - id: 3
      value: error
      color: error
```

Same shape and validation as priorities. Law frontmatter uses `severity: <value>` and the loader resolves that label through the active enum registry.

Adding `{id: 4, value: blocker, color: error}` makes `severity: blocker` a valid law frontmatter value after the next bundle load.

## Runtime wiring

`app.ConfigService.Import` returns a `*domain.EnumRegistry` after `LoadBundle` and before `config.BuildSnapshot`. Composition roots inject the registry into services that resolve labels. Wire format stores integer ids; label projection happens at DTO/render boundaries.

There are no process-global enum tables. Tests use registry helpers from `internal/testfixtures`.

## Update when

- Priority/severity fields or validation rules change.
- A new configurable enum table is added.
- Template default precedence changes.
- Runtime enum registry wiring changes.
