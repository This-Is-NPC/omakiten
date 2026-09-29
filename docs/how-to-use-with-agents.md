# How to give an agent the same context

**The question:** how does an agent recover the board and leave useful state
for the next session?

The integration is one skill, `omakiten`, teaching the installed `okt` CLI.
It is embedded in the binary. The agent needs shell access to `okt` and the
project's database and workflow.

## Install the skill

```bash
okt setup --cli-lang en --agent-lang en --preset omakase --harnesses agents
```

`agents` installs `~/.agents/skills/omakiten/SKILL.md`. Add `claude-code` to
publish the same content under `~/.claude/skills/omakiten/SKILL.md`.
`--skip-harnesses` skips publication; the install environment accepts
`OKT_HARNESSES=0` for that choice too.

For project-local discovery:

```bash
okt init --name Example --slug example --root "$PWD" --skill
```

Add `--claude-code` for the project-local Claude Code path. Managed updates
refresh the owned skill entrypoint and preserve neighboring files. A foreign
skill at that path is a conflict rather than something setup overwrites.

## Recover before acting

```bash
okt --project example project resume
okt --project example task continue 42
okt --project example comment list 42
okt --project example search "reload failure"
```

Use explicit project selection. A handoff is useful only if the next session
reads the same work you wrote it on.

## Read a command from the active workflow

```bash
okt --project example command list
okt --project example command resolve NAME
```

Resolution composes the selected persona, skills, laws, and templates. The
response includes `data.markdown`, the structured entities, and immediate
related commands at the detail level declared by the selected command. Read
the list first: names and parameters are defined by the active workflow.
Pass `--arguments` with a JSON object when the selected command declares
parameters.
Resolution supplies instructions; it does not execute a coding session.

The repository's rules and the user's request still apply. Follow guard
failures and permissions rather than manufacturing evidence to pass them.

## Leave a checkpoint

```bash
okt --project example comment add 42 --author agent --kind handoff \
  --body 'Focused tests pass. Review the translated error before merging.'
```

Record the current state, validation, remaining blocker, and next action.
Use task comments for task evidence, project comments for project-wide state,
and errors and solutions for reusable failures and fixes.

`OMAKITEN_AGENT_MODEL` records the actual model identifier on CLI activity;
`OMAKITEN_AGENT_SESSION_ID` can identify the session. Atomic plan claims
require a nonempty model identifier. These values describe provenance, not
permission to bypass the workflow.

## Check the result

Data commands return an `ok` envelope and a nonzero exit status on failure.
Read both. Help, completion, interactive screens, and document exports have
their own output formats; [the CLI guide](cli.md) explains those boundaries.
