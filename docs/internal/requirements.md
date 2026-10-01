# The behavior the project promises

These are current product and maintenance constraints. They describe observable
behavior and the owner of each rule, rather than a historical feature ledger.

## Work and project scope

| Requirement | Owner |
| --- | --- |
| Explicit project selectors resolve before working-directory inference; unresolved explicit selectors fail. | `internal/project`, `internal/operation` |
| Tasks, parents, dependencies, plans, and scoped evidence cannot mix projects. | `internal/app`, `internal/sqlite` |
| Dependency and parent relationships cannot form cycles. | `internal/graph`, application validation, SQLite constraints |
| Workflow transitions and permissions come from the active project snapshot. | `internal/app/guards`, `internal/domain/workflow.go` |
| Task creation reports similar work before a confirmed duplicate creation. | Task intent operations |
| A plan claim atomically assigns eligible first-bucket work and leaves transitions to workflow guards. | Plan services and SQLite claim transaction |
| Errors and solutions are reusable across projects, with actual confirmation outcomes. | Error and solution services |

## Files, policy, and language

| Requirement | Owner |
| --- | --- |
| A preset captures all package files and runs independently of its source checkout. | Package installer and configuration loader |
| Agent commands are discovered from the selected workflow's command skills and bindings; resolving one includes only its declared immediate context. | `internal/config`, `internal/operation` |
| Package paths, regular files, manifest, configuration, and references are validated on installation. | `internal/config`, `internal/installer` |
| Package installation does not inspect or execute hook scripts. | Installer boundary |
| Successful edits publish one validated package snapshot; failures leave selection intact. | Bundle editor and `internal/configstore` |
| Project configuration replaces the selected local policy without changing application-wide language preferences. | Scope resolution and preferences |
| Translation catalogs ship in the application; language commands can repair preferences without a valid workflow or database. | `internal/config`, CLI language commands |
| Each project runtime owns immutable policy and event registries; a failed reload preserves the accepted runtime. | `internal/agentruntime` |

## Documents, recovery, and delivery

| Requirement | Owner |
| --- | --- |
| A task or plan export is one UTF-8 OKF Markdown file, with versioned Omakiten structure. | `internal/workfile`, work document services |
| Structured imports are all-or-nothing, including relationships, metadata, and business events; dry-run rolls back. | Work document repository transaction |
| Exported relationships must fit inside the document's task set. | Export validation |
| Project knowledge is read from files without SQLite persistence; related projects are followed only when declared. | `internal/knowledgefile`, CLI and TUI knowledge ports |
| Destructive database operations use verified live recovery images and protected directory identity. | SQLite and recovery adapters |
| Database backup, search reindex, and project deletion are census operations; shipped surfaces keep backup and reindex off HTTP, and serve project deletion behind a confirmation. | `internal/operation`, `agentruntime.Maintenance`, surfaces table |
| Search repair is transactional and verified against canonical records. | Search integrity adapter |
| CLI data output is machine-readable, errors exit nonzero, and diagnostics preserve actionable context. | CLI execution and output |
| CLI and TUI use common contracts and application policy while remaining independent delivery adapters. | Composition root and architecture tests |
| The HTTP API listens on loopback only, authenticates with a per-run token, and gates each route by its surface slug. | `internal/httpapi`, `internal/daemon`, surfaces table |
| Every operation the shipped surfaces table enables on HTTP has a route, so the API keeps parity with the CLI. | `internal/httpapi` route coverage test |
| The HTTP API's OpenAPI document is generated from its routes and delivery types and reviewed as a golden file. | `internal/httpapi` |
| The event stream delivers events committed by any process, resumes from Last-Event-ID, and asks slow clients to resync. | `internal/daemon` feed and `internal/httpapi` hub |
| TUI screens receive business projections; shared components own geometry, cursor, scroll, and framing. | Screen assembly contract |
| Release verification checks the intended repository, tag, identity, metadata, and complete archive set. | Release metadata and verification packages |

## Maintenance

Mise owns the pinned toolchain and project tasks. Each task delegates to its
own script; the pre-push hook runs the merge gate. Generated output belongs
under `.tmp/`, with persistent development state under `dev_env/`.

Keep tests that protect these behaviors, including failure paths and transaction
boundaries. Package import restrictions are enforced by `internal/arch` and
lint. The [contribution checklist](../../CONTRIBUTING.md) and
[TUI assembly rules](tui-screen-assembly.md) define the implementation contract.
