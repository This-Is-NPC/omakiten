# How to change a workflow

**The question:** where do I change policy, instructions, and reusable assets
without making a second configuration system inside Omakiten?

Start from a workflow repository or an exported package. Keep related rules in
their owning modules. Install and select the validated package when it is ready.

## 1. Find the entry and its modules

The root `preset.yaml` names the config entry. A modular entry can read:

```yaml
version: 1
kit: {id: 101, key: example, name: Example Workflow}
config: {from: ./settings.yaml}
workflows: {from: ./workflows.yaml}
surfaces: {from: ./surfaces.yaml}
skills: {from: ./catalog.yaml#skills}
laws: {from: ./catalog.yaml#laws}
personas: {from: ./personas.yaml}
commands: {from: ./bindings.yaml}
```

`from` replaces a value with the selected source value. `merge_from` merges a
mapping and lets the importing mapping's values win. Relative paths resolve
from the containing file. A fragment selects a key in the imported YAML.
References are bounded by the package root; cycles are rejected.

Use compact YAML rows for short records and scalar lists. Use blocks for
nested policy and multiline text. YAML does not have a separate table syntax.

## 2. Change the part that owns the behavior

| Module or folder | What belongs there |
| --- | --- |
| `settings.yaml` | Output shape, active workflow, agent response limits, SQLite tuning, backups, themes, views, search, and event settings. |
| `workflows.yaml` | Ordered buckets, transitions, permissions, guards, and subtask kit selection. |
| `surfaces.yaml` | Which operations are exposed to CLI and TUI. |
| `catalog.yaml` | Skill and law references. |
| `personas.yaml` and `personas/` | Persona references, role text, skill repertoire, and themed presentation. |
| `bindings.yaml` | Command bindings to personas, skills, laws, and templates. |
| `skills/`, `laws/`, `templates/` | Markdown assets with YAML frontmatter. |
| `themes/` | Color theme YAML. |
| `notifications/` | Notification card YAML used by hooks. |

Asset slugs come from the package's entity files and wiring. Inspect examples
in the source preset before adding fields. The CLI families `skill`, `law`,
and `persona` edit through the shared package editor. `template list` and
`template show` inspect the resolved templates.

Application translations and language preferences are configured through
Omakiten itself. They do not go in these settings.

## 3. Keep the state machine explicit

This is a workflow fragment, to place in an otherwise complete package:

```yaml
buckets:
  - {id: 1, key: backlog, name: Backlog, position: 1}
  - {id: 2, key: dev, name: Development, position: 2}
  - {id: 3, key: done, name: Done, position: 3}
transitions:
  - {from: 1, to: 2}
  - {from: 2, to: 3}
```

Bucket ids are unique within the workflow and are stored on tasks. Preserve
them when renaming a bucket with existing work. The first ordered bucket is
the creation default; the final ordered bucket represents completion. Moves
must follow declared transitions.

Task permissions cover edit and delete. Comment permissions cover create,
edit, and delete, with scope-specific defaults. An explicit `false` blocks;
an omitted setting follows the configured fallback chain.

### Evidence before a move

| Guard | What it requires |
| --- | --- |
| `blockers_in` | Dependencies are in permitted buckets. |
| `comments_min` | A minimum number of comments. |
| `comments_tagged` | Comments carrying the configured evidence tags. |
| `wave_gate` | Earlier plan waves allow this task to proceed. |
| `subtasks_complete` | Child tasks are complete. |

Guards can apply to transitions and supported operations. Copy a guard from
the selected repository to keep its argument shape correct, then change the
condition and its actionable hint. `${{intl:KEY}}` resolves bundled translation
text; literal hints are also available.

Subtask kits declare the workflow at the next task level. Parent links remain
project-local. Changing kit or bucket identity can require rebinding existing
work; inspect `okt workflow orphans --help` before applying a migration.

## 4. Bind instructions to a command

```bash
okt --project example command list
okt --project example command resolve okt-task-review
```

The binding composes persona instructions, skills, laws, and templates.
Inspect the resolved Markdown after changing one of those assets. A template
can contribute its bound laws; a persona can contribute its skill repertoire.
Resolution reads instructions and does not execute them.

## 5. Validate and publish the selection

```bash
okt config validate workflow/config/preset.yaml
okt --project example preset add ./workflow
okt --project example preset list
okt --project example preset use example
```

Select the reported content id if the name has several installed revisions.
Studio offers preview and apply for supported edits. Both Studio and the CLI
use staged package publication; a failed candidate is not activated.

Finish by inspecting `workflow show` and resolving the commands you changed.
To distribute the result, use [preset export](how-to-manage-presets.md).
