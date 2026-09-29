# How to keep and recover the board

**The question:** where is the work stored, how do I save it, and what should
I do before a destructive operation?

The board is a SQLite database. Workflow packages and language preferences
are files outside it. Keep both when you want to recover the same work under
the same policy.

## Write a live snapshot

```bash
okt db backup
```

The command snapshots the live SQLite connection, including committed data
still in the WAL. Its JSON result reports the written path. Retention comes
from the active backup settings; a retention count of zero disables pruning.

Use the returned database image for a copy or restore. Copying only the live
`omakiten.db` file while writers are active can miss committed WAL data.

## Preserve the policy with it

```bash
okt --project example preset export --output workflow.md
okt --project example plan export delivery --output delivery.md
```

The first captures the workflow package. The second is a portable copy of one
plan, rather than a complete database backup. Application preferences can be
saved separately from the path reported by `config language show`.

## Open a recovery copy

```bash
okt --db recovered.db projects list
okt --db recovered.db --project example project resume
```

Use the snapshot's actual path. This selects a different database for that
invocation. Before replacing the installed database, stop running TUI and CLI
writers, keep the current database and its sidecars as a recovery copy, and
restore the standalone snapshot. Omakiten has no `db restore` subcommand.

The selected workflow must still match the recovered work. If buckets were
changed, inspect `workflow orphans` before rebinding tasks.

## Check and repair search

```bash
okt db check
okt db reindex --help
```

`check` compares the search index with canonical data. Confirmed reindexing
creates a recovery image and rebuilds the index in a transaction. Failed
validation or repair rolls back the change. Reindexing repairs search; it
does not repair arbitrary database or configuration damage.

## Before deleting a project

Read `okt projects delete --help` and inspect the project explicitly. Project
deletion creates a recovery image before its transactional cascade. That image
is evidence of the database generation used for deletion, including the WAL.
Keep the reported path until you have verified the result.

For a portable subset instead, export the relevant tasks and plans before
deletion. [Work documents](how-to-import-and-export-work.md) explain which
relationships must fit inside an export.
