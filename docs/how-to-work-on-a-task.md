---
relates_to: [cli:task, cli:task.create, cli:task.continue]
---

# How to take a task from an idea to done

**The question:** how do I keep the work, the evidence, and the next action
together instead of leaving them in a terminal session?

The examples assume a registered project named `example`. `42` stands for
the task id returned by creation. Bucket keys come from the active workflow.

## 1. Read before creating

```bash
okt --project example project resume
okt --project example search "configuration reload"
okt --project example workflow show
```

Search finds tasks, comments, plans, errors, and solutions. Read related work
before creating a second task for the same problem.

## 2. Write the task

```bash
okt --project example task create --title "Make reload failures actionable" \
  --description "Show the failing path and the next recovery command."
```

The first bucket of the active workflow is the default. `--bucket` and
`--priority` accept configured keys and labels. `--parent 42` makes the new
task a child of an existing task in the same project.

If creation reports similar work, inspect it. `--confirm` confirms creation
after that review. A Markdown file can supply the task instead of flags; see
[import and export](how-to-import-and-export-work.md).

## 3. Read the checkpoint and start work

```bash
okt --project example task continue 42
okt --project example comment list 42
```

Continuation includes workflow context and related history. In Omakase, the
transition into development requires a tagged branch comment. Record the
branch you are actually using, then request the transition:

```bash
okt --project example comment add 42 --tag self-branch \
  --body 'Branch: fix/actionable-reload'
okt --project example move 42 --to dev
```

Other presets can require different evidence. A guard failure names the
missing condition. Satisfy it before trying again.

## 4. Keep decisions and evidence on the task

```bash
okt --project example comment add 42 --kind decision \
  --body 'Keep the selected runtime active when the replacement is invalid.'
okt --project example comment add 42 --kind handoff \
  --body 'Implementation complete. Focused tests pass. Next: review error wording.'
```

Use `--author agent` when an agent writes the comment. Project-scope comments
use `--scope project` without a task id. Keep a reusable failure and fix with
`okt error` and `okt solution`; their help describes the required fields.

Dependencies are explicit: inspect `okt depend --help` to add or remove a
blocker. They must stay inside the project and cannot form cycles.

## 5. Finish through the workflow

```bash
okt --project example move 42 --to review
okt --project example move 42 --to done
```

Those keys are the Omakase path. Review evidence, unfinished children, plan
waves, and permissions can stop a move. Completing a task is a workflow
transition; assigning it to somebody is a separate operation.

`okt archive 42` archives work; `okt unarchive 42` restores it. Hard deletion
uses `okt delete` and its explicit confirmation flag. Archived tasks must be
unarchived before editing. A plan deletion detaches its member tasks rather
than deleting them.

## Next

[Move a plan as one file](how-to-import-and-export-work.md) when the work has
ordered waves. [Use the terminal](screens.md) when you want the same board
with visible focus, activity, and details.
