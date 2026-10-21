# How to give a project its own workflow

**The question:** what happens when a repository has a `.omakiten/` directory,
and which parts still belong to the user's Omakiten installation?

The directory is a complete project configuration scope. It selects an installed
preset snapshot. The application's database and language preferences have their
own roots.

## 1. Create the local selection

```bash
okt --project example config init --scope local --preset omakase
okt --project example config path --scope local
okt --project example config show --scope local
```

The installation has this shape:

```text
.omakiten/
  config.yaml
  presets/
    <content-id>/
      preset.yaml
      config/
      skills/
      laws/
      personas/
      templates/
      themes/
      notifications/
```

`config.yaml` points at a captured package's config entry. Runtime loading
reads that snapshot. Removing the original workflow checkout does not remove
the installed package.

## 2. Know what wins

An explicit `--config` path takes precedence. Otherwise Omakiten discovers
repository-local `.omakiten/` configuration before the user-wide selection.
Discovery walks up from the repository location and respects repository and
filesystem boundaries. An invalid discovered installation is an error, rather
than a reason to silently use another project's policy.

Project selection uses `--project-id`, then `--project`, then the registered
root matching the working directory. Use explicit selectors when inspecting
another project.

```bash
okt --project example config path
okt --project example config why config.workflow.active
okt --project example config validate
```

`config show` prints the selected YAML; imports can put the owning values in
other modules. `config why` resolves a dotted key through those imports.
`config diff LEFT RIGHT` compares two configuration sources structurally.

## What stays application-wide

| State | Default root |
| --- | --- |
| User-wide configuration and preferences | `$XDG_CONFIG_HOME/omakiten`, or `~/.config/omakiten` |
| Operational database | `$XDG_DATA_HOME/omakiten`, or `~/.local/share/omakiten` |
| Recovery state | `$XDG_STATE_HOME/omakiten`, or `~/.local/state/omakiten` |

`OMAKITEN_HOME` places configuration under that root, the database under
`<root>/data/`, and recovery state under `<root>/state/`. `--db` selects an
explicit database. A project-local workflow selection does not change the
application preference path or make the database project-local.

Language preferences live in `<application config root>/preferences.yaml`.
Changing language applies across projects and leaves preset identities intact.
The same file holds your agent adapters and session limits; see
[agents](how-to-run-agent-teams.md#4-choose-harnesses).

## How a change becomes active

Each project runtime owns its workflow snapshot, registries, services, and hook
engine. Reload prepares a replacement before publishing it. A load or consumer
validation failure leaves the accepted runtime active and reports the error.
One project's replacement does not change another project's policy.

Use the CLI or Studio to edit installed entities. For file-level workflow
changes, work in the package's source repository and install the candidate.
[Preset management](how-to-manage-presets.md) explains snapshot identities and
modified copies.

## Share the scope

Commit the project installation when its selected package should travel with
the repository, or export the preset as one file and let each user install it.
Tasks and plans are shared through their own
[work documents](how-to-import-and-export-work.md), rather than preset files.

## Repair a moved project

After moving the directory, update its registration using its existing slug or id:

```bash
okt --project example project relocate --root /absolute/new/project
okt --project example project overview
```

The operation updates only the registered root. It preserves the project's id,
slug, name, description, tasks, plans, comments, and history. It does not move or
create files, change the slug, or relocate other registered projects nested in
the old directory. Move the repository and its `.omakiten/` directory yourself
before calling it. The old directory may already be missing.

The target must be an existing directory with an absolute path and no symlink
components. A root already registered to another project, including an archived
one, is rejected with `project_path_conflict`. A concurrent root change produces
the same error; read `projects list` before retrying. Repeating the current path
succeeds without another audit event.

The root change and its `project.updated` event commit together. The event
contains `root_path.from` and `root_path.to`; an audit-write failure rolls back
the change. The response includes `previous_root_path` and the updated project.

Recovery uses the application workflow's `project.relocate` surface permission
instead of loading policy through the old root. An explicit CLI `--config`
overrides that application workflow. HTTP uses the daemon session's permission
and exposes `POST /api/v1/projects/{project}/relocate` with a JSON body such as
`{"root_path":"/absolute/new/project"}`. The API path is on the server's filesystem.

Subsequent requests discover configuration at the new root and replace a cached
runtime when its configuration path changes. An invalid configuration at the new
location is reported on those requests; relocation itself does not validate or
modify that configuration. Update the workflow source and install a compatible
preset containing the `project.relocate` surface entry before using this operation.
