# When a command refuses

A useful refusal names what failed and the next action. Start with the coded
error and reported path. Data commands return those details in JSON; a terminal
also receives a concise diagnostic on stderr.

## Setup succeeded, but the next command says `config_invalid`

Run these from the directory where the failure happens:

```bash
okt config path
okt config validate
```

Global setup and project-local discovery can select different installations.
Inspect the reported file rather than replacing the global installation again.
`--config PATH` selects a file explicitly while you investigate.

For a scope you want to initialize from an official repository:

```bash
okt config init --scope local --preset omakase --force
```

Use `--scope global` for the application installation. `--force` authorizes
reinitializing that selection. Export valuable custom work before replacing
its active policy. `config refresh-defaults` refreshes managed defaults and
skills; it is not a request to discard a modified preset.

## The language preference is invalid

```bash
okt config language reset
okt config language set --cli en --tui en --agent en
```

These commands operate on application preferences without opening the workflow
or database. `config language show` reports the supported codes.

## A move reports `guard_violation`

```bash
okt --project example task continue 42
okt --project example comment list 42
okt --project example workflow show
```

Read the failing guard and its hint. It may require review evidence, completed
children, a dependency in a permitted bucket, or completion of an earlier wave.
Record the real evidence or resolve the blocker, then repeat the move.

An operation permission failure is different: the workflow can forbid editing
or deleting in a particular bucket. Changing the request's surface does not
make that permission disappear.

## Tasks are stranded after selecting another preset

```bash
okt --project example workflow orphans
okt --project example workflow orphans --help
```

Preview the affected tasks, then use the documented rebind options. Selecting
a preset alone does not change existing task bucket identities.

## An import fails

Use `--dry-run` to inspect the complete candidate. Check UTF-8, frontmatter,
profile version, unique file-local keys, and references. Bucket and priority
values must exist in the selected workflow. A failed structured import leaves
no partially created plan, tasks, relationships, or business events.

If export refuses an existing path, choose another path or supply `--force`.
If it reports an external relationship, include the related work in the
document scope before exporting.

## A command opens an editor or waits for confirmation

Read that command's `--help` for its noninteractive inputs and confirmation
flags. Do not pipe affirmative answers blindly. An agent should pass the
documented flags after reviewing the operation it is confirming.

## The result belongs to another project

Run `okt projects list` and supply `--project` or `--project-id` explicitly.
Also check `config path`: a directory above the checkout can contain a
project-local installation that is being discovered.

## Search results disagree with the board

Run `okt db check`. Follow [database recovery](how-to-back-up-and-recover.md)
for a confirmed transactional reindex, and retain the recovery image.
