---
relates_to: [cli:init, cli:projects]
---

# How to put a project on the board

**The question:** how does Omakiten know which repository I am working on,
and how do I come back to it without reconstructing the whole session?

You need an installed `okt` and a selected workflow. A project is a registered
root directory with a name and a slug; its tasks live in the application
database.

## 1. Register the root

From the repository:

```bash
okt init --name Example --slug example --root "$PWD"
```

Keep the returned project id and slug. Registering a project does not require
an agent, a plan, or a remote Git repository.

`--skill` also installs the Omakiten skill inside the registered root. Add
`--claude-code` for the Claude Code destination. To install a project workflow
at initialization, supply `--preset omakase`; `--preset-force` refreshes its
managed content.

## 2. Check what was selected

```bash
okt projects list
okt --project example project overview
okt --project example workflow show
```

`--project-id` selects by numeric id. Without an explicit selector, Omakiten
matches the working directory to a registered root. An explicit id takes
precedence over a slug. Naming a project that does not exist is an error.

Use an explicit selector in automation, especially when a process can run
outside the checkout it is meant to change.

## 3. Recover context

```bash
okt --project example project resume
okt --project example list
```

Resume gathers the project's current work and checkpoint information. To
continue one task, use `okt --project example task continue 42`.

The [task guide](how-to-work-on-a-task.md) covers the next loop: take work,
record evidence, move it through the workflow, and leave a handoff.

## Give this project its own policy

```bash
okt config init --scope local --preset omakase
```

This creates a `.omakiten/` installation in the selected project. It changes
workflow selection for that project; it does not create another database or
change application language preferences. Read
[project configuration](how-to-configure-a-project.md) before sharing it.

## When the result is about another project

Run `okt projects list` and select the intended slug explicitly. Then inspect
`okt config path` to see where configuration is coming from. Project selection
and configuration selection are related, but neither is proof that the other
is the one you expected.
