---
name: Authorize remote writes
severity: error
---
Never run `git push`, `git push --force`, `gh pr create`, `gh pr edit`, `gh pr merge`, or any command that publishes or mutates a remote repository without explicit user authorization in the current conversation. Local commits, branches, and file edits are fine. Each authorization is scoped — pushing one branch does not authorize future pushes; opening one PR does not authorize others.
