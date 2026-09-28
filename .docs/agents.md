# Agent integration

Omakiten installs one Agent Skills entrypoint, `omakiten`, to teach agents the
`okt` CLI. The skill is shipped inside the binary and is available without a
network fetch. The agent needs shell access to the installed `okt` executable.

## Install

`okt setup` selects language, preset and skill destinations. The shared
destination is `~/.agents/skills/omakiten/SKILL.md`; Claude Code uses
`~/.claude/skills/omakiten/SKILL.md`. Both contain the same skill.

```sh
okt setup --cli-lang en --agent-lang en --preset omakase --harnesses agents,claude-code
okt setup --update --cli-lang en --agent-lang en --preset omakase --harnesses agents
```

`OKT_HARNESSES=agents,claude-code` selects the destinations through the bootstrap
installer. `0` skips skill installation. `--skip-harnesses` skips publishing
the skill while installing development configuration.

For a project-local installation:

```sh
okt init --name Example --slug example --root /path/to/project --skill
okt init --name Example --slug example --root /path/to/project --skill --claude-code
```

The local paths are `.agents/skills/omakiten/SKILL.md` and, when selected,
`.claude/skills/omakiten/SKILL.md` below the registered root. `--preset-force`
refreshes managed local content. A foreign skill at either destination is a
conflict, even during an update.

`okt update` refreshes managed skills through its defaults refresh step;
`--skip-defaults` also skips skill refresh. Missing and foreign skills remain
untouched. `okt config refresh-defaults` performs the same refresh directly.

`okt uninstall --yes` removes the managed global skill entrypoints with the
binary and shell wrapper. It preserves neighboring files and user configuration
and databases unless a purge flag is supplied. Project-local skills remain
with their projects.

## Use

```sh
okt --project example project resume
okt --project example task continue 42
okt --project example command list
okt --project example command resolve okt-task-implement --arguments '{"task_id":42}'
okt --project example comment add 42 --author agent --kind handoff --body 'Validation passed; ready for review.'
```

`command resolve` composes the active persona, skills, laws and templates from
the `commands:` bindings. Its JSON response includes `data.markdown` and the
structured entities. It reads context; it does not execute the playbook.

Data commands return a JSON envelope and unsuccessful operations exit nonzero.
`okt <command> --help` describes the current arguments and confirmation flags.
Workflow guards apply to agent CLI calls just as they do to other callers.
