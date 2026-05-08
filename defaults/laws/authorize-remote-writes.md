---
name: Authorize remote writes
severity: error
---
Never run `git push`, `git push --force`, `gh pr create`, `gh pr edit`, `gh pr merge`, or any command publishing/mutating a remote repo without explicit user authorization in this conversation. Local commits, branches, file edits OK. Authorization is per-action: pushing one branch does not authorize future pushes; opening one PR does not authorize others.
