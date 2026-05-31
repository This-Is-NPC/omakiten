# Presets — side-by-side comparison

Omakiten ships four official workflow presets. Pick one at install time (`okt setup --preset <name>`) or per-project under `.omakiten/config/.active`.

The YAML under `defaults/config/<preset>.yaml` is the source of truth for shipped defaults. This page compares workflow discipline and guard shape only. For YAML schemas, start at [configuration-guide/README.md](configuration-guide/README.md). For command roles/scopes, see [command-surface.md](command-surface.md).

## At a glance

| Preset | Discipline | Buckets | Forward guards | Best for |
|---|---|---|---|---|
| **🍻 izakaya** | Lean spike / tracer-bullet / walking skeleton | `backlog → dev → done` | `#hypothesis`, `wave_gate` on `backlog→dev` only | Solo experiments, prototypes, time-boxed spikes. |
| **🍱 omakase** | Trunk-based development + DORA + TDD | `backlog → dev → review → done` | `#self-branch`, `blockers_in`, `#resume`, `wave_gate`, `subtasks_complete` | Small teams shipping continuously to main. |
| **🎌 kaiseki** | Staged delivery with formal sign-offs | `requirements → planning → dev → review → docs → done` | Requirements, acceptance, design, peer review, documentation, `subtasks_complete` | Regulated work, multi-stakeholder coordination. |
| **🥢 shokunin** | SRE + pre-mortem + multi-reviewer change control | `requirements → planning → dev → review → docs → done` | Kaiseki plus pre-mortem, risk assessment, rollback plan, dual review, lessons learned | Production-critical paths where blast radius matters. |

## How presets differ

Per-preset workflow behavior is derived from `defaults/config/<preset>.yaml`. What changes by discipline level is:

- **`workflows[].buckets` / `transitions` / `operations`** — bucket count, forward gates, regression paths, and destructive-operation guards.
- **Guard vocabulary** — each discipline level adds checks without prescribing code architecture.

Inspect any preset's full wiring when you need exact active slugs:

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

For smaller changes, prefer editing the relevant config module over whole-preset switching. See [configuration-guide/README.md](configuration-guide/README.md).

## Update when

- A new official preset lands under `defaults/config/<preset>.yaml` — add a row to [At a glance](#at-a-glance) and a guidance entry to [Picking a preset](#picking-a-preset).
- A preset's bucket count or guard set changes meaningfully (sanity-check the row in the comparison table).
- The selection guidance shifts (new discipline, new vocabulary).

## See also

- [workflow.md](workflow.md) — conceptual loop and per-preset walkthrough.
- [command-surface.md](command-surface.md) — stable command roles, scopes, and write behavior.
- [configuration-guide/workflows.md](configuration-guide/workflows.md) — workflow schema.
- [configuration-guide/command-bindings.md](configuration-guide/command-bindings.md) — command role/skill/law/template binding schema.
- [configuration-guide/guards.md](configuration-guide/guards.md) — the guard types these presets compose.
- `defaults/config/<preset>.yaml` — source of truth for shipped defaults.
