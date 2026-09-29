# How to install Omakiten, and take it off again

**The question:** how do I get `okt` onto a machine, what does setup put there,
and what happens to my work when I uninstall it?

This page covers installation. Registering the first project is the
[next step](how-to-register-a-project.md).

## Install a release

On Linux, macOS, or WSL:

```bash
curl -fsSL https://raw.githubusercontent.com/This-Is-NPC/omakiten/master/install.sh | bash
```

On Windows, in PowerShell:

```powershell
irm https://raw.githubusercontent.com/This-Is-NPC/omakiten/master/install.ps1 | iex
```

The installer downloads the release for your operating system and architecture,
checks its checksum, installs the binary, and opens `okt setup`. Git is needed
to fetch a workflow repository. The default binary destination is
`~/.local/bin/okt` on Unix and `%LOCALAPPDATA%\Programs\okt` on Windows.
`INSTALL_DIR` changes it; `VERSION` selects a particular release.

### Bootstrap trust model

The default verification mode checks the archive against the release's
published checksum. The bootstrap script and release metadata are fetched
from GitHub over HTTPS.

For authenticated release verification, install Cosign 3 or newer yourself.
The Bash strict path also needs `jq`. Download the bootstrap script, inspect
it, then run it with strict verification:

```bash
curl -fsSL -o install.sh https://raw.githubusercontent.com/This-Is-NPC/omakiten/master/install.sh
OKT_VERIFY_MODE=strict bash install.sh
```

In PowerShell, download `install.ps1`, set `$env:OKT_VERIFY_MODE = "strict"`,
and run the file. `OKT_COSIGN` can name the verifier executable explicitly.
Strict mode checks the signing identity, release manifest, provenance, and
archive digests before extraction. A failed check stops installation.

## Make the setup choices

```bash
okt setup
```

The picker asks for the interface language, agent output language, workflow,
and skill destinations. CLI and TUI share the setup language choice; you can
change them separately afterward. The `agents` destination publishes the
shared skill; `claude-code` publishes the Claude Code entrypoint.

For a setup with those choices already supplied:

```bash
okt setup --cli-lang en --tui-lang en --agent-lang en \
  --preset omakase --harnesses agents
```

Setup writes a shell wrapper, application preferences, and a selected workflow
snapshot. The wrapper supports changing directory after leaving the TUI.
`--skip-wrapper` and `--skip-harnesses` leave those setup steps out.

| What | Default Unix location |
| --- | --- |
| Application preferences and workflow selection | `~/.config/omakiten/` |
| Installed preset snapshots | `~/.config/omakiten/presets/<id>/` |
| Operational database | `~/.local/share/omakiten/omakiten.db` |
| Shared agent skill | `~/.agents/skills/omakiten/SKILL.md` |
| Claude Code skill | `~/.claude/skills/omakiten/SKILL.md` |

XDG variables or `OMAKITEN_HOME` can move the application roots. The complete
scope rules are in [project configuration](how-to-configure-a-project.md).

## From a checkout

```bash
git clone https://github.com/This-Is-NPC/omakiten.git
cd omakiten
mise install
mise run install
```

This builds the current branch, installs `okt`, runs setup with update behavior,
and registers the checkout. To develop against isolated state instead, use
`mise run tui`; it keeps configuration and the database under `dev_env/`.

## Update

```bash
okt update
okt setup --update
```

The first command updates the installed executable. The second revisits setup
choices. Setup refreshes pristine preset snapshots from their recorded sources
and preserves the selected modified preset. Source repositories are needed
when fetching an update, rather than when running an installed workflow.

### Update trust model

`okt update --help` describes the update verification options. The updater
verifies the downloaded release and stages the replacement on the executable's
filesystem before swapping it. Read its result before treating the update as
complete. Updating the executable does not migrate task buckets to a different
workflow.

## Take it off

```bash
okt uninstall --yes
```

This removes the installed binary, its shell wrapper, and owned global skill
entrypoints. Your configuration and database remain. Project-local skills
belong to their projects and remain there too.

`--purge-config` deletes application configuration; `--purge-data` deletes the
database and related data; `--purge` selects both. Back up the board before
using any purge option. The checkout tasks `mise run uninstall` and
`mise run purge` ask for confirmation.

## When setup fails

Read the reported path before replacing configuration. A command run from a
repository can discover a project-local installation even after global setup
succeeded. [When a command refuses](troubleshooting.md) explains how to inspect
that scope and initialize a fresh selection.
