# How to move a plan as one file

**The question:** can I write a task in Markdown, or carry a complete plan
between projects without managing a directory of separate task files?

Yes. A task can start from plain Markdown. Structured tasks and plans use
one OKF document: YAML frontmatter for relationships, Markdown for the body.
The Omakiten profile is under `omakiten`, at version `1`.

## A task from plain Markdown

Write `resume.md`:

```markdown
Make reload failures actionable

Show the failing configuration path and a command the user can run next.
```

Then create it:

```bash
okt --project example task create --file resume.md
```

Without `--title`, the first line supplies the title. The file content remains
the task description. Flags such as `--title`,
`--bucket`, `--priority`, and `--parent` let you supply task fields explicitly.
`--file -` reads standard input.

Structured files supply their own title, priority, bucket, parent, and template
fields; those creation flags cannot override the structured document.

## A complete plan

Write `delivery.md`:

```markdown
---
type: Omakiten Plan
title: Actionable configuration errors
omakiten:
  version: 1
  slug: delivery
  waves:
    - key: foundation
      name: Foundation
      tasks:
        - {key: inspect, title: Identify the failing configuration path}
    - key: delivery
      name: Delivery
      tasks:
        - key: explain
          title: Show the recovery command
          depends_on: [inspect]
---
## Goal

Every configuration failure gives the user a useful next action.
```

Preview the complete import, then create the plan:

```bash
okt --project example plan import --file delivery.md --dry-run
okt --project example plan create --file delivery.md
```

`plan import` also creates the complete plan. The importer creates waves,
tasks, parent relationships, tags, and dependencies in one transaction.
An invalid reference or late validation failure rolls back the whole import.
A preview rolls back its writes and events too.

## A structured task and its children

```markdown
---
type: Omakiten Task
title: Improve configuration diagnostics
omakiten:
  version: 1
  task: {key: diagnostics}
  tasks:
    - {key: path, title: Display the selected path, parent: diagnostics}
---
Explain the failure and the next recovery action.
```

Use `okt task import --file task.md --dry-run`, then import without
`--dry-run`. Review similarity hints before using `--confirm`.

Keys are local to the document; they are not database ids. `parent` and
`depends_on` must name tasks in that file. Priority labels and bucket keys
must exist in the target workflow. The file must be UTF-8 and at most 16 MiB.

## Export one file

```bash
okt --project example plan export delivery --output delivery.md
okt --project example task export 42 --output task.md
```

Without `--output`, export writes raw Markdown to stdout. An existing output
file requires `--force`. Export includes the work's current content and
retained producer metadata, rather than depending on the original input file.

A task export includes its descendants; a plan export includes its waves and
members. Parents and outgoing dependencies must be represented inside the
document. A relationship to work outside the export is reported as an error.

## Work the plan

```bash
okt --project example plan show delivery
okt --project example plan continue delivery
```

Continue previews the next claimable task without assigning it. For agent
coordination, set `OMAKITEN_AGENT_MODEL` to the agent's actual identity and use
`okt plan claim delivery`. Claim atomically assigns eligible work; it does
not move the task into development. That move still follows workflow guards.

Use `plan wave-add` and `plan assign` to build a plan from existing tasks.
`okt plan --help` lists the wave-editing and membership commands.
