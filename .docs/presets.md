# Presets — side-by-side comparison

Omakiten ships four official workflow presets. Each is a complete kit: workflow shape, persona allowlist, law set, guard configuration, and MCP-command bindings. Pick one at install time (`okt setup --preset <name>`) or per-project under `.omakiten/config/.active`.

The YAML under `defaults/config/<preset>.yaml` is the source of truth for everything below. This page compares them side by side so you can pick. For the conceptual loop, see [workflow.md](workflow.md). For the YAML schema, see [configuration-guide/entities.md § workflows](configuration-guide/entities.md#workflows).

## At a glance

| Preset | Discipline | Buckets | Forward guards | Best for |
|---|---|---|---|---|
| **🍻 izakaya** | Lean spike / tracer-bullet / walking skeleton | `backlog → dev → done` | `#hypothesis`, `wave_gate` on `backlog→dev` only | Solo experiments, prototypes, time-boxed spikes. |
| **🍱 omakase** | Trunk-based development + DORA + TDD | `backlog → dev → review → done` | `#self-branch`, `blockers_in`, `#resume`, `wave_gate`, `subtasks_complete` | Small teams shipping continuously to main. |
| **🎌 kaiseki** | Staged delivery with formal sign-offs | `backlog → analysis → dev → review → staging → done` | Multi-stage gates, ADR pointer, sign-off tags | Regulated work, multi-stakeholder coordination. |
| **🥢 shokunin** | SRE + pre-mortem + multi-reviewer change control | `backlog → dev → review → canary → done` | Pre-mortem, two-reviewer, error-budget guard | Production-critical paths where blast radius matters. |

## How presets differ

Per-preset wiring (personas, laws, MCP-command bindings, workflow guards) is derived from `defaults/config/<preset>.yaml`. The shape is identical across all four — what changes is the **content** of each list:

- **`personas:`** — strict allowlist; entities outside the list still live on disk but do not load. izakaya keeps it minimal (tinkerer + check-runner + reviewer + commit-author + documentation-agent); kaiseki and shokunin add stage-specific roles (architect, release-manager, sre, qa-gatekeeper, etc.).
- **`laws:`** — global laws inherited by every command. The four presets agree on `template-fidelity`, `authorize-remote-writes`, `project-scope-only`, and `workflow-enforced`; preset-specific laws layer on top (e.g. omakase's `green-main-always`, shokunin's `blameless-postmortem`).
- **`mcp_commands.<command>.laws` / `templates`** — per-command bindings. Each command (`okt-create`, `okt-implement`, `okt-review`, …) gets a persona, a law set on top of `global`, and one or more templates. This is where presets express their character — izakaya's `okt-implement` adds `time-boxed-spike` + `tracer-bullet`; shokunin's adds `change-management` + `pre-mortem-required`.
- **`workflows[].buckets` / `transitions` / `operations`** — bucket count and guard layout. Omakase has the canonical 4-bucket trunk; kaiseki and shokunin add review / canary / staging buckets with stage-specific guards.

Inspect any preset's full wiring:

```bash
yq '.' defaults/config/omakase.yaml          # full preset YAML
okt config init --preset izakaya --scope local --force
okt config show --scope local                # render the active file
```

## Picking a preset

| You want to… | Use |
|---|---|
| Spike a feature in a day, throw away half. | **izakaya** |
| Ship to main multiple times a week with TDD + green CI. | **omakase** |
| Cross stage gates with documented hand-offs and sign-offs. | **kaiseki** |
| Run production-critical work with pre-mortem + multi-reviewer. | **shokunin** |

## Switching between presets

Presets live as separate YAML files under `<config-root>/config/`. The `.active` state file picks one:

```bash
echo "kaiseki.yaml" > "$HOME/.config/omakiten/config/.active"
okt config validate
```

Existing tasks keep their `bucket_id` — but bucket ids are workflow-scoped. Switching presets does NOT migrate task state. Re-bucket via `okt move <id> --to <new-bucket>` per task, or absorb the work as a one-time migration before the switch.

## Update when

- A new official preset lands under `defaults/config/<preset>.yaml` — add a row to [At a glance](#at-a-glance) and a guidance entry to [Picking a preset](#picking-a-preset).
- A preset's bucket count, guard set, or persona allowlist changes meaningfully (sanity-check the row in the comparison table).
- The selection guidance shifts (new discipline, new vocabulary).

## See also

- [workflow.md](workflow.md) — conceptual loop and per-preset walkthrough.
- [configuration-guide/entities.md](configuration-guide/entities.md) — full schema for the YAML each preset emits.
- [configuration-guide/guards.md](configuration-guide/guards.md) — the guard types these presets compose.
- `defaults/config/<preset>.yaml` — source of truth for every wiring decision.
