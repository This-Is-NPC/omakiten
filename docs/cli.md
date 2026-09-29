# The command line

`okt` reads and writes the same board the terminal displays. Start with a
command family, then ask that command for the flags it accepts:

```bash
okt --help
okt task --help
okt task create --help
```

Help comes from the executable's command tree. This page is a map of that
tree and its output contract; the [how-to pages](README.md) show complete
procedures.

## Select the scope

| Global flag | Meaning |
| --- | --- |
| `--project`, `-p` | Select a registered project by slug. |
| `--project-id` | Select by numeric project id; takes precedence over the slug. |
| `--config` | Load an explicit configuration path. |
| `--db` | Open an explicit database path. |
| `--help`, `-h` | Show command help. |
| `--version`, `-v` | Show the executable version at the root command. |

Without an explicit project, Omakiten resolves the registered root from the
working directory. Configuration discovery is described in
[project configuration](how-to-configure-a-project.md).

## Find the verb

| Family or command | What it does |
| --- | --- |
| `setup`, `update`, `uninstall` | Install choices, executable updates, and removal. |
| `init`, `projects list`, `projects delete` | Register, discover, and remove projects. |
| `project overview`, `project resume`, `project edit` | Read project state, recover context, and edit its description. |
| `task create`, `task import`, `task export` | Create work and move structured work documents. |
| `task continue`, `task activity` | Recover a task and read its activity. |
| `list`, `edit`, `move` | List work, change fields, and request a workflow transition. |
| `assign`, `archive`, `unarchive`, `delete` | Assignment and task lifecycle. |
| `comment add`, `list`, `edit`, `delete` | Task, project, and universal comments. |
| `depend add`, `remove`, `list` | Explicit task blockers. |
| `tag add`, `remove`, `list`, `list-all`, `merge` | Task and project tags. |
| `plan create`, `import`, `export`, `show`, `list`, `edit`, `delete` | Plans and their portable documents. |
| `plan wave-add`, `wave-remove`, `wave-rename`, `wave-reorder` | Ordered plan waves. |
| `plan assign`, `unassign`, `continue`, `claim` | Membership, next-work preview, and atomic assignment. |
| `search` | Full-text search across operational records. |
| `error record` | Reusable development failures. |
| `solution add`, `confirm`, `list-top` | Fixes, observed outcomes, and ranked reuse. |
| `progress` | Material progress on a task. |
| `insights summary`, `metrics summary` | Project signals and agent behavior metrics. |
| `logs` | Inspect the unified event history. |
| `workflow show`, `workflow orphans` | Read policy and inspect or rebind stranded bucket identities. |
| `preset catalog`, `add`, `list`, `use`, `import`, `export` | Discover, capture, select, and share workflow packages. |
| `config init`, `path`, `show`, `why`, `diff`, `validate` | Initialize and inspect configuration. |
| `config language show`, `set`, `reset` | Application language preferences. |
| `config refresh-defaults`, `config surfaces` | Managed refresh and operation-surface inspection. |
| `skill`, `law`, `persona`, `template` | File-backed instruction assets. |
| `command list`, `command resolve` | Discover and compose configured agent playbooks. |
| `db backup`, `check`, `reindex` | Recovery snapshots and search-index maintenance. |
| `completion` | Shell completion from the command tree. |
| `tui` | Open the interactive terminal. |

## Read the result

Data operations write one JSON envelope to stdout:

```json
{"ok":true,"data":{"result":"example"}}
```

A failure carries a code and message, with optional details:

```json
{"ok":false,"code":"validation_error","msg":"example failure","details":{}}
```

Failures exit nonzero. Validate both the exit status and `ok` before using
`data`. In a terminal, diagnostics on stderr include the failing operation's
help or remediation command where applicable. Keep stderr separate when
consuming JSON in a script.

Help, version, shell completion, and interactive setup/TUI have their own
text output. `config show` prints YAML. Successful task, plan, and preset
exports write raw Markdown to stdout unless an output file is supplied.

## Automation

Commands that can open an editor or ask for confirmation provide explicit
inputs in their help. Read similarity hints, deletion targets, and import
previews before supplying a confirmation flag.

| Environment | Meaning |
| --- | --- |
| `OMAKITEN_HOME` | Application configuration root; data and recovery state are beneath it. |
| `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME` | Platform roots when `OMAKITEN_HOME` is unset. |
| `OMAKITEN_AGENT_MODEL` | Actual agent model identity recorded with CLI activity; required for plan claims. |
| `OMAKITEN_AGENT_SESSION_ID` | Optional session identity for correlated agent metrics. |
| `OKT_CLI_LANG`, `OKT_TUI_LANG`, `OKT_AGENT_LANG` | Setup language inputs. |
| `OKT_PRESET`, `OKT_HARNESSES` | Setup workflow and skill-destination inputs. |
| `OKT_CD_FILE` | TUI shell-wrapper directory-change handshake path. |

For completion, run `okt completion SHELL --help` for your shell. Completion
uses the same command definitions as help.
