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
