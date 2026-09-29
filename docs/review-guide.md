# Reviewing a change

Start with the concrete behavior the change promises. Follow its owner through
the [design](design.md), then read the failure path as carefully as the happy
path. The [requirements](requirements.md) name the invariants that must hold.

## Classify the consequence

| Severity | Consequence |
| --- | --- |
| Critical | Data loss, unsafe destructive behavior, or a release verification bypass. |
| High | Broken project isolation, invalid state publication, or a user unable to complete a supported operation. |
| Medium | Incorrect presentation, misleading diagnostics, or avoidable repeated work with observable impact. |
| Low | A concrete maintainability or clarity defect with limited behavioral impact. |

State the trigger, expected behavior, observed behavior, and evidence. Cite the
owning code or a reproducible command. Label an inference as an inference.

## Verify at the boundary

Exercise existing tests for the affected behavior. Structural changes also run
`internal/arch`. Use the project tasks for formatting, lint, shell, and workspace
checks; the pre-push gate verifies clean HEAD before publication.

For transaction changes, inspect rollback and event publication. For runtime
changes, inspect candidate rejection, drain, and shutdown. For TUI changes,
exercise real navigation, resize, and stale replies rather than asserting a
copied calculation. For CLI changes, inspect scope, output streams, exit status,
and the next action a failure gives the user.

## Route the finding

Report findings to the Third Hokage. It classifies each as a deviation within
the task in flight or new work that earns its own task. Other agents report
the evidence rather than creating parallel review tasks.

Keep the checkpoint on the board through `okt`: outcome, validation, blockers,
and next action. PR descriptions use repository-relative files and public
references that a reviewer can open without access to a local installation.
