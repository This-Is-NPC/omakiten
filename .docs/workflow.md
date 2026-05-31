# Workflow Guide

Every team works differently, and any one preset will feel wrong to half its users. Omakiten ships four official workflow presets and a path for users to author their own. Each preset embodies a documented software-engineering process — peer review, decision records, test evidence, audit trail, lessons learned — without prescribing architectural patterns. Whether the project is hexagonal, layered, MVC, event-driven, or something else, the workflow stays the same.

This guide is the authoritative reference for picking a preset, understanding what it enforces, and forking your own.

## Contents

- [Why presets exist](#why-presets-exist)
- [How to pick a preset](#how-to-pick-a-preset)
- [PDCA mapping — the cycle behind every preset](#pdca-mapping--the-cycle-behind-every-preset)
- [🍻 izakaya — Lean spike, tracer-bullet, walking skeleton](#-izakaya--lean-spike-tracer-bullet-walking-skeleton)
- [🍱 omakase — Trunk-based development with DORA discipline](#-omakase--trunk-based-development-with-dora-discipline) (canonical worked example)
- [🎌 kaiseki — Staged delivery with formal sign-offs](#-kaiseki--staged-delivery-with-formal-sign-offs)
- [🥢 shokunin — Site Reliability Engineering with multi-reviewer change control](#-shokunin--site-reliability-engineering-with-multi-reviewer-change-control)
- [Cross-preset progression](#cross-preset-progression)
- [Notes and handoff loop](#notes-and-handoff-loop)
- [Plans — multi-agent fan-out](#plans--multi-agent-fan-out)
- [Authoring your own preset](#authoring-your-own-preset)
- [See also](#see-also)

## Why presets exist

A preset is the combination of:

- **persona** — the role the agent assumes (mindset, default behaviour)
- **laws** — process rules the agent must follow
- **skills** — capability bundles bound to the persona
- **templates** — task / comment / decision-record scaffolds
- **workflow shape** — buckets, transitions, guards, permissions, operations

Picking a preset is picking a process discipline level. Picking architecture is a per-codebase decision the workflow stays out of.

The canonical kit is **omakase**. The other three (`izakaya`, `kaiseki`, `shokunin`) sit at different points on the discipline spectrum: less ceremony than omakase (izakaya), more ceremony with formal sign-offs (kaiseki), maximum ceremony with multi-reviewer change control (shokunin). Fork any of them when none fit.

## How to pick a preset

Three questions decide most of it.

| Question | izakaya | omakase | kaiseki | shokunin |
|---|---|---|---|---|
| Cycle time matters more than audit trail? | yes | balanced | no | no |
| Need formal sign-offs / recorded decisions? | no | optional | yes | yes |
| Failures affect users, money, compliance? | low | medium | medium | high |

Quick decision matrix:

- **Spike, prototype, side-project, personal task** → izakaya
- **Mainstream professional software work, small-batch shipping, balanced rigor** → omakase
- **Planned features in a serious codebase, decision records, peer review required** → kaiseki
- **Regulated environment, multi-reviewer sign-off, blameless postmortems, rollback discipline** → shokunin

When in doubt, pick **omakase**. It is the canonical kit and the default selection at install time.

---

## PDCA mapping — the cycle behind every preset

Each preset embodies a different process discipline level, but every preset runs the same underlying cycle: [PDCA — Plan-Do-Check-Act](./why_omakiten.md#pdca). Every action through Omakiten maps onto one of the four phases.

The user-facing orchestrator loop is `okt-start -> okt-shape -> okt-run -> okt-audit -> okt-pause`. Under that loop, granular task commands map to PDCA phases:

| PDCA phase | `okt-*` command | What happens |
|---|---|---|
| **PLAN** | `okt-task-imagine` | Owner role interrogates the user via [5W2H](./why_omakiten.md#5w2h). Define success in [SMART](./why_omakiten.md#smart) terms. Surface assumptions and gaps. Decide if the request is concrete enough to file. |
| **PLAN → DO** | `okt-task-create` | Formalize the imagined work as a task. [INVEST](./why_omakiten.md#invest) checklist on the user story. Acceptance criteria. Prioritization ([MoSCoW](./why_omakiten.md#moscow) or [RICE](./why_omakiten.md#rice)) when alternatives exist. Non-functional requirements named separately when relevant. |
| **DO** | task in `dev`, `okt-task-continue`, early `okt-task-implement` | Execute the planned increment. Test-first, conventional commits, small batches (the engineering discipline each preset enforces). |
| **ACT** | mid `okt-task-implement` | Adjust during execution — drive-by cleanup, decision records on divergence, refactors, escalate when guards block. |
| **CHECK** | end of `okt-task-implement` → task in `review` → `done` | Verify the outcome against the SMART success metric defined in PLAN. Peer review. Tests passing. Promote to `done` only when the loop closes. |

`okt-task-document` and `okt-config` sit outside the main loop — they orient the agent (CHECK-flavored) and read-only.

### What each phase produces

| Phase | Artifacts a user sees at the end of the phase |
|---|---|
| PLAN | `#5w2h` comment, `#acceptance` comment, `#smart-success` comment (or `#hypothesis` in izakaya). Task body filled per the chosen `task-*` template. |
| DO | Code commits (conventional format), branch tagged via `#self-branch` comment, `#design` comment (kaiseki) or `#pre-mortem` + `#risk-assessment` (shokunin). |
| ACT | Decision records, drive-by refactor comments, ADR-style files at `docs/decisions/` (or the project's preferred location). |
| CHECK | `#resume` comment, `#tests-passing` comment with evidence, `#peer-review` comment(s), `#documentation` comment, `#lessons-learned` comment (shokunin). |

### Cross-discipline coherence

Three disciplines ride together at the preset's chosen level:

- **Software engineering** (trunk-based development / TDD / SRE / decision records) — how code lands.
- **Product management** ([5W2H](./why_omakiten.md#5w2h) / [SMART](./why_omakiten.md#smart) / [INVEST](./why_omakiten.md#invest) / [MoSCoW](./why_omakiten.md#moscow) / [RICE](./why_omakiten.md#rice) / outcomes) — what gets built and why.
- **Project management** ([PDCA](./why_omakiten.md#pdca) cycle awareness, staged delivery, audit trail) — how the work is structured and recorded.

A preset is **coherent** when its engineering rigor matches its product rigor matches its project-management rigor. izakaya keeps all three light (spike); omakase balances all three at mainstream professional level; kaiseki tightens all three with formal stages; shokunin elevates all three with audit-trail integrity and multi-reviewer sign-off.

---

## 🍻 izakaya — Lean spike, tracer-bullet, walking skeleton

> Hypothesis-driven exploration. Build only what proves the question. Kill, promote, or extend explicitly when the time-box runs out.

### Methodology basis

- **Lean Startup** — [Ries 2011](./why_omakiten.md#ries-2011): build-measure-learn loops; MVP design.
- **Extreme Programming (XP) Spike** — [Beck & Andres 2004](./why_omakiten.md#beck-andres-2004): time-boxed exploratory work, throwaway code, learning-first.
- **Tracer Bullet** — [Hunt & Thomas 1999](./why_omakiten.md#hunt-thomas-1999) ch.7: thin end-to-end slice before depth.
- **Walking Skeleton** — [Cockburn — Crystal Clear](./why_omakiten.md#cockburn-crystal): connect the wires first, deepen later.

### Workflow shape

```
backlog ──▶ dev ──▶ done
   ▲        ▲       │
   │        └───────┘
   └─ regressions: dev→backlog, done→dev, done→backlog
```

### Permissions

| Bucket | task.edit | task.delete | comment.edit | comment.delete |
|---|---|---|---|---|
| backlog | ✅ | ✅ | ✅ | ✅ |
| dev | ✅ | ✅ | ✅ | ✅ |
| done | ✅ | ✅ | ✅ | ✅ |

Maximum permissiveness — spikes need to reshape freely.

### Guards

| Transition | Guards |
|---|---|
| backlog → dev | `comments_tagged: hypothesis count=1` · `wave_gate` |
| dev → done | — |
| dev → backlog | — |
| done → dev | — |
| done → backlog | — |

Operations: no guards (archive / delete / unarchive free).

### Delta vs omakase

Lean spike kit: discovery, creation, and implementation can all bind to the same lightweight role. `okt-task-create` may use a spike-shaped task template with hypothesis-first laws (no INVEST, no SMART). `okt-task-implement` runs under time-boxed/tracer-bullet constraints (no test-evidence, no green-main). `okt-task-check` and `okt-task-review` accept yellow / time-boxed findings. Exact entity bindings live in the active config, not in this guide.

### Visible output

| Aspect | What it looks like |
|---|---|
| Branch naming | `spike/<topic>` |
| PR? | Optional — single-author spikes can merge direct |
| Commits | Free-form |
| Repo structure | `experiments/` or `spikes/` is common |
| Comments | "Hypothesis / Evidence / Verdict" shape |
| Failure handling | Discard + document via `#discard` |

### How to fork

```bash
cp ~/.config/omakiten/config/izakaya.yaml ~/.config/omakiten/config/custom/my-izakaya.yaml
echo my-izakaya.yaml > ~/.config/omakiten/config/.active
# edit the copy, run `okt config validate` until clean
```

---

## 🍱 omakase — Trunk-based development with DORA discipline

> Mainstream professional software work. Small batches, green main always, test evidence on every behavioral change, opportunistic cleanup.

### Methodology basis

- **Trunk-Based Development** — short-lived branches, fast revert, feature flags.
- **Continuous Integration** — [Fowler 2006](./why_omakiten.md#fowler-ci-2006): green main as the source of truth.
- **DORA** — [Forsgren, Humble & Kim 2018](./why_omakiten.md#forsgren-2018): lead time, deploy frequency, MTTR, change failure rate as the four optimization targets.
- **Test-Driven Development** — [Beck 2002](./why_omakiten.md#beck-tdd-2002): red → green → refactor; tests-first on new behavior.
- **Conventional Commits** — [conventionalcommits.org](./why_omakiten.md#conventional-commits): machine-parseable commit messages.
- **Boy Scout Rule** — [Martin — Clean Code 2008](./why_omakiten.md#martin-clean-2008) p.14: leave code cleaner than you found it.

### Workflow shape

```
backlog ──▶ dev ──▶ review ──▶ done
   ▲        ▲        ▲          │
   │        │        └──────────┤  done→review
   │        └───────────────────┤  done→dev
   └────────────────────────────┘  done→backlog
                                   (also: dev→backlog, review→dev,
                                          review→backlog)
```

### Permissions

| Bucket | task.edit | task.delete | comment.edit | comment.delete |
|---|---|---|---|---|
| backlog | ✅ | ✅ | ✅ | ✅ |
| dev | ❌ (inherit) | ❌ (inherit) | ✅ | ✅ |
| review | ❌ | ❌ | ✅ | ❌ |
| done | ❌ | ❌ (explicit) | ❌ | ❌ (explicit) |

### Guards

| Transition | Guards |
|---|---|
| backlog → dev | `comments_tagged: self-branch count=1` · `blockers_in: [done]` · `wave_gate` |
| dev → review | `comments_tagged: resume count=1` · `comments_tagged: tests-passing count=1` · `subtasks_complete` |
| review → done | `comments_tagged: documentation count=1` |
| regressions (6 paths) | — |

Operations: archive requires `#documentation`.

### Persona, laws, skills, templates

Personas, MCP-command bindings, and workflow guards for `omakase` live in `defaults/config/omakase.yaml`. Inspect the active wiring with:

```bash
okt persona list                     # personas in the active preset
okt mcp call templates.list --input '{}'
yq '.workflows[0]' defaults/config/omakase.yaml
```

The [presets comparison](./presets.md) gives the side-by-side view across all four official presets.

Severity (`error` vs `warning`) per law lives in its frontmatter under `defaults/laws/<slug>.md` (or the user override in `<root>/laws/custom/<slug>.md`). Inspect via `okt law show <slug>`.

### Visible output

| Aspect | What it looks like |
|---|---|
| Branch naming | `feature/<short-name>`, `fix/<short-name>` |
| PR? | Mandatory, <400 LOC per PR |
| Commits | Conventional, granular, many small |
| Repo structure | Project convention; no preset prescription |
| Comments | `#tests-passing` with command + output + duration; optional `#refactor-drive-by` |
| Failure handling | Revert or fix-forward within 10 minutes |

### How to fork

```bash
cp ~/.config/omakiten/config/omakase.yaml ~/.config/omakiten/config/custom/my-omakase.yaml
echo my-omakase.yaml > ~/.config/omakiten/config/.active
```

---

## 🎌 kaiseki — Staged delivery with formal sign-offs

> Six-stage flow with explicit gates and recorded decisions. Architecture-agnostic; the preset says *when* to write things down, not *what* architecture to write.

### Methodology basis

- **PMBOK Guide** — [PMI](./why_omakiten.md#pmi-pmbok): stages, gates, sign-offs, change control.
- **Pressman** — [*Software Engineering: A Practitioner's Approach*](./why_omakiten.md#pressman): staged lifecycle models.
- **Royce 1970** — [*Managing the Development of Large Software Systems*](./why_omakiten.md#royce-1970): origin of waterfall + iterative refinement.
- **ISO/IEC 12207** — [software lifecycle processes](./why_omakiten.md#iso-12207).

### Workflow shape

```
requirements ──▶ planning ──▶ dev ──▶ review ──▶ docs ──▶ done
                                       ▲          ▲         │
                                       └──────────┴─────────┤  done→review
                                                            │  done→docs
                                       (also: review→dev,
                                              docs→review)
```

### Permissions

| Bucket | task.edit | task.delete | comment.edit | comment.delete |
|---|---|---|---|---|
| requirements | ✅ | ✅ | ✅ | ✅ |
| planning | ✅ | ✅ | ✅ | ❌ |
| dev | ❌ (inherit) | ❌ (inherit) | ✅ | ✅ |
| review | ❌ | ❌ | ❌ | ❌ |
| docs | ❌ | ❌ | ❌ | ❌ |
| done | ❌ | ❌ | ❌ | ❌ |

### Guards

| Transition | Guards |
|---|---|
| requirements → planning | `comments_tagged: 5w2h` · `comments_tagged: requirements` · `comments_tagged: acceptance` |
| planning → dev | `comments_tagged: self-branch` · `comments_tagged: design` · `blockers_in: [done, docs]` · `wave_gate` |
| dev → review | `comments_tagged: resume` · `comments_tagged: tests-passing` · `subtasks_complete` |
| review → docs | `comments_tagged: peer-review` |
| docs → done | `comments_tagged: documentation` · `subtasks_complete` |
| regressions | review→dev, docs→review, done→review, done→docs |

Operations: archive requires `#documentation`; delete requires `#peer-review`.

### Delta vs omakase

Adds two upstream buckets (`requirements`, `planning`) and one downstream bucket (`docs`). The Owner role spends more time on requirements and acceptance criteria; the Builder role tightens toward staged delivery, design documentation, and decision records. `okt-task-create`, `okt-task-implement`, and `okt-task-review` can bind stricter laws/templates in the active config. Operations: archive requires `#documentation`, delete requires `#peer-review`.

### Visible output

| Aspect | What it looks like |
|---|---|
| Branch naming | `feature/<context>-<scope>` |
| PR? | Mandatory, with `#peer-review` sign-off |
| Commits | Conventional, granular, one intent per commit |
| Repo structure | `docs/decisions/` or `docs/design/` populated as features land |
| Comments | `#requirements`, `#acceptance` (any testable shape), `#design`, `#peer-review`, `#documentation` |
| Failure handling | Regression transitions back to dev / review / docs |

The decision-record format is the project's call — Nygard ADR, RFC, design doc, sketch. Kaiseki only enforces *that* the decision is written down.

### How to fork

```bash
cp ~/.config/omakiten/config/kaiseki.yaml ~/.config/omakiten/config/custom/my-kaiseki.yaml
echo my-kaiseki.yaml > ~/.config/omakiten/config/.active
```

---

## 🥢 shokunin — Site Reliability Engineering with multi-reviewer change control

> Treats every change as regulated. Pre-mortem, rollback plan, two independent reviewers, blameless postmortem, append-only audit trail.

### Methodology basis

- **Site Reliability Engineering** — [Beyer et al. 2016](./why_omakiten.md#beyer-2016): SLI / SLO / error budgets, four golden signals.
- **Pre-mortem** — [Klein 2007](./why_omakiten.md#klein-2007): imagine the failure before shipping.
- **Blameless Postmortems** — [Allspaw 2012](./why_omakiten.md#allspaw-2012): no "human error" as root cause.
- **Continuous Delivery** — [Humble & Farley 2010](./why_omakiten.md#humble-farley-2010): release gates, multi-reviewer sign-off, rollback-first design.

### Workflow shape

```
requirements ──▶ planning ──▶ dev ──▶ review ──▶ docs ──▶ done
                                       ▲          ▲         │
                                       │          └─────────┤  done→review
                                       └────────────────────┤
                                                            │  (review→dev,
                                                            │   docs→review)
```

### Permissions

| Bucket | task.edit | task.delete | comment.edit | comment.delete |
|---|---|---|---|---|
| requirements | ✅ | ❌ | ✅ | ❌ |
| planning | ❌ (inherit) | ❌ | ✅ | ❌ |
| dev | ❌ | ❌ | ✅ | ❌ |
| review | ❌ | ❌ | ❌ | ❌ |
| docs | ❌ | ❌ | ❌ | ❌ |
| done | ❌ | ❌ | ❌ | ❌ |

All `comment.delete` is denied workflow-wide — audit trail must survive. Corrections happen via `#scribe-correction` (append-only).

### Guards

| Transition | Guards |
|---|---|
| requirements → planning | `comments_tagged: 5w2h` · `comments_tagged: requirements` · `comments_tagged: acceptance` |
| planning → dev | `comments_tagged: self-branch` · `comments_tagged: pre-mortem` · `comments_tagged: risk-assessment` · `blockers_in: [done, docs]` · `wave_gate` |
| dev → review | `comments_tagged: resume` · `comments_tagged: tests-passing` · `comments_tagged: rollback-plan` · `subtasks_complete` |
| review → docs | `comments_tagged: peer-review count=2` |
| docs → done | `comments_tagged: documentation` · `comments_tagged: lessons-learned` |
| regressions | review→dev, docs→review, done→review |

Operations: archive requires `#documentation` + `#lessons-learned`; delete requires `#peer-review`; unarchive requires `#peer-review`.

### Delta vs kaiseki

Same six-bucket shape, but every gate is tightened. The Owner role frames blast radius and error-budget impact; the Builder role hardens toward SRE-grade change control; Tester and Reviewer roles can bind stricter check/review laws. Guards: `planning → dev` adds `#pre-mortem` + `#risk-assessment`; `dev → review` adds `#rollback-plan`; `review → docs` requires `#peer-review`×**2**; `docs → done` adds `#lessons-learned`. Operations: archive requires `#documentation` + `#lessons-learned`; delete and unarchive both require `#peer-review`.

### Visible output

| Aspect | What it looks like |
|---|---|
| Branch naming | `change/<id>-<title>` |
| PR? | Mandatory, with two `#peer-review` sign-offs, rollback section, pre-mortem section, coverage delta |
| Commits | Conventional, granular, often signed |
| Repo structure | `docs/decisions/`, `docs/postmortems/`, `docs/runbooks/` populated over time |
| Comments | Append-only; corrections via `#scribe-correction` |
| Failure handling | Blameless `#postmortem` for every incident or near-miss |

### How to fork

```bash
cp ~/.config/omakiten/config/shokunin.yaml ~/.config/omakiten/config/custom/my-shokunin.yaml
echo my-shokunin.yaml > ~/.config/omakiten/config/.active
```

---

## Cross-preset progression

| Quality gate | izakaya | omakase | kaiseki | shokunin |
|---|---|---|---|---|
| Branch tagging (`#self-branch`) | — | ✅ | ✅ | ✅ |
| Hypothesis required (`#hypothesis`) | ✅ | — | — | — |
| Requirements signed off (`#requirements`) | — | — | ✅ | ✅ |
| Acceptance criteria (`#acceptance`) | — | — | ✅ | ✅ |
| Design recorded (`#design`) | — | — | ✅ | — |
| Pre-mortem (`#pre-mortem`) | — | — | — | ✅ |
| Risk assessment (`#risk-assessment`) | — | — | — | ✅ |
| Blockers cleared (`blockers_in`) | — | ✅ | ✅ | ✅ |
| Wave gating (`wave_gate`) | ✅ | ✅ | ✅ | ✅ |
| Resume handoff (`#resume`) | — | ✅ | ✅ | ✅ |
| Test evidence (`#tests-passing`) | — | ✅ | ✅ | ✅ |
| Rollback plan (`#rollback-plan`) | — | — | — | ✅ |
| Peer review (`#peer-review`, count) | — | — | 1 | **2** |
| Documentation (`#documentation`) | — | ✅ | ✅ | ✅ |
| Lessons learned (`#lessons-learned`) | — | — | — | ✅ |
| Archive guard | — | ✅ | ✅ | ✅ |
| Delete guard | — | — | ✅ | ✅ |
| Unarchive guard | — | — | — | ✅ |
| Comment trail preserved (delete denied) | — | dev+ | planning+ | requirements+ |

Each level adds one or more layers of discipline without removing the previous ones.

---

## Notes and handoff loop

Sitting alongside the bucket cycle, a small set of atomic commands carry knowledge between sessions and across projects. They are wired into every preset (`omakase`, `izakaya`, `kaiseki`, `shokunin`) and read or write through the **scope-aware comment model** — there is no separate notes entity. A "note" is just a comment whose `scope` is `project` or `universal` (rather than `task`), optionally carrying a `title`, a free-form `kind` (`handoff`, `recap`, …), and a `pinned` flag. The same `events` table and FTS5 search index back both task comments and these project/global notes.

### The commands

| Command | Phase | Writes? | Purpose |
|---|---|---|---|
| `okt-pause` | session close (CHECK) | `comments.add scope=project kind=handoff` | Synthesise the session: delta since the previous handoff, active work, recent progress, decisions/discards, blockers, next steps. Bound to the `note-handoff` template; carries `project-scope-only` + `no-praise-pad` laws so the body stays factual. |
| `okt-note-free` | any (ACT/CHECK) | `comments.add` (free `kind`) | Ad-hoc knowledge capture — gotcha, decision, glossary entry, anything that should outlive the current task. Bound to `note-free` (minimal title/body/footer shell). Scope `project` (default) or `global` → `universal` (cross-project visibility). |
| `okt-note-recap` | retrospective / session start | read-only | Temporal window over project notes grouped by `kind`. A wide (cross-project) window folds in the handoff digest: the latest `kind=handoff` comment per project plus the delta since. Read-side aggregation against `comments.list`. Bound to `note-recap` / `note-standup-digest`. |
| `okt-note-list` / `okt-note-show` | any | read-only | List or display project/global notes via `comments.list` filtered by `scope`/`kind`/`pinned`. |

### The loop in practice

```
session start ──▶  okt-note-recap (wide)   (read: latest handoff per project)
                       │
                       ▼
                   okt-project-resume / okt-task-continue
                       │
                   …work…                       (bucket cycle: dev → review → done)
                       │
                       ▼
session close ──▶  okt-pause        (write: handoff comment, scope=project kind=handoff)
                       │
                       ▼
periodic     ──▶  okt-note-recap    (read: window summary)
                       │
                       ▼
ad-hoc       ──▶  okt-note-free     (write: any time something deserves to outlive the task)
```

Two write commands (`okt-pause`, `okt-note-free`) and the read family (`okt-note-recap`, `okt-note-list`, `okt-note-show`). The read commands need no per-preset wiring deltas — they aggregate whatever project/universal comments the `events` table already holds. The write commands carry a thin law set (`project-scope-only`, `no-praise-pad` on `okt-pause`) so the synthesised body stays auditable without inflating the prompt budget.

### Task comments vs. project/global notes

A note is the same comment row with a wider scope — the distinction is `scope`, not a separate table:

| Task comment (`scope=task`) | Project/global note (`scope=project` / `universal`) |
|---|---|
| Lives inside one task's audit trail | Lives alongside the project, or globally (`universal`) |
| Bucket `permissions.comment.*` gate edit / delete (inherits task policy) | Edit / delete gated task-lessly by `workflows[].defaults.comment.{project,universal}.*` |
| Cascade-deleted with the task | Outlives any single task |
| Indexed in `search` under `entity_type="comment"` | Indexed under `entity_type="comment"`, with `title` searchable too |

A task's `#tests-passing` evidence belongs in the task's comment trail (it documents that task's CHECK phase). A "we picked SQLite over Postgres because…" decision, the team glossary, a runbook for a flaky deploy, or the snapshot at the end of today's session — those belong in a `scope=project`/`universal` comment, where they stay reachable from any future task or project view.

See [`mcp.md` § Comments & activity](./mcp.md#comments--activity) for the tool-level surface (scope/kind/title/pinned filters) and [`mcp.md` § Prompts](./mcp.md#prompts) for the prompt table including these commands.

---

## Plans — multi-agent fan-out

Plans sit **on top of** the active workflow, not inside it. A plan groups child tasks into ordered **waves** so that two to four AI agents can fan out across the same goal without racing each other. The workflow shape (bucket cycle, transition guards, permissions) is unchanged — plans add a coordination layer.

### The wave-gate rule

Tasks inside the same wave run in parallel. Wave `N+1` is blocked until wave `N` is fully closed. The rule is enforced by the `wave_gate` guard (`internal/app/guards/evaluator.go:checkWaveGate`) registered alongside `blockers_in`, `comments_min`, and `comments_tagged` — see [Guards Guide § `wave_gate`](./configuration-guide/guards.md#wave_gate).

The four official presets all wire `wave_gate` onto the transition that enters `dev`:

| Preset | Transition |
|---|---|
| izakaya | `backlog → dev` |
| omakase | `backlog → dev` |
| kaiseki | `planning → dev` |
| shokunin | `planning → dev` |

Tasks not attached to a plan (`wave_id IS NULL`) pass the guard as a no-op, so the new guard is safe in every existing preset.

### Atomic claim — `plans.claim_next`

`plans.claim_next` is the only correct way for an agent to acquire work inside a plan. The MCP tool wraps a single SQLite write transaction:

1. `BEGIN IMMEDIATE` — serialises against any concurrent claim attempt.
2. `SELECT` the active wave (lowest-position wave with any non-final active task), then the lowest-id task in that wave that is active, unassigned, and still in the workflow's first bucket.
3. `UPDATE tasks SET assigned_to=<caller _agent_model>` in the same transaction.
4. Commit. Returns the claimed task or `{claimed: false}`. The bucket is not moved.

Two concurrent calls land on the same write lock; the loser retries the SELECT and either claims a different first-bucket task or returns empty. No double-claim is possible. The CLI handle (`okt plan claim <slug>`) hits the same primitive.

Agents claim first, then move with `tasks.move` after preset-defined guard preconditions are satisfied. Calling `tasks.move` without a prior claim bypasses the assignment write and leaves the activity log inconsistent with the plan's progress view.

### Recovery from a crashed claim

`tasks.assigned_to` stays set if the claiming agent crashes mid-task. Recovery is deliberately human-driven:

- `okt assign <task_id>` (no `WHO`) clears the assignment.
- `okt move <task_id> backlog` clears it via the transition-out hook.

v1 does not auto-reclaim — silent reclaim would hide real-world agent failures. A v2 path is on the roadmap.

### Surfaces

- **MCP**: 13 tools under `plans.*` (`create`, `list`, `show`, `add_wave`, `assign_task`, `continue`, `claim_next`, `edit`, `delete`, `remove_wave`, `rename_wave`, `reorder_wave`, `unassign`). See [MCP Guide § Plans](./mcp.md#plans-wbs-style-multi-agent-orchestration).
- **CLI**: `okt plan create|list|show|wave-add|assign|claim|edit|delete|wave-remove|wave-rename|wave-reorder|unassign` and the orthogonal `okt assign <task_id> [who]` for free-text assignment outside the plan flow. See [CLI Guide § Plans](./cli.md#plans).
- **TUI**: a fourth sub-tab under `01 // TASKS` — list view first, then a column-per-wave network diagram per plan. See [TUI Guide § Tasks › Plans](./tui.md#tasks--plans).
- **Search**: `plans.goal_body` is indexed in the unified FTS5 `search_index` so cross-project `search` finds plans by name or any phrase in the goal markdown.

---

## Authoring your own preset

The four official presets are starting points. Authoring a preset means choosing a process discipline, then wiring the matching configuration modules:

| Concern | Canonical guide |
|---|---|
| Workflow buckets, transitions, operations, permissions | [`configuration-guide/workflows.md`](./configuration-guide/workflows.md) |
| Guard payloads and failures | [`configuration-guide/guards.md`](./configuration-guide/guards.md) |
| Command-to-role/skill/law/template bindings | [`configuration-guide/command-bindings.md`](./configuration-guide/command-bindings.md) |
| Skill/law/persona/template asset frontmatter | [`configuration-guide/entities.md`](./configuration-guide/entities.md) |
| Splitting sections with `from:` imports | [`configuration-guide/path-resolution.md`](./configuration-guide/path-resolution.md#modular-imports) |

### What workflows do NOT prescribe

- **Architectural patterns** — layered, hexagonal, MVC, DDD, event-driven, microservices. These are per-codebase decisions. The workflow stays neutral.
- **Programming language conventions** — naming, file layout, formatter choice. Use the project's existing conventions; the workflow does not override.
- **Tooling** — CI provider, test framework, lint rules. The workflow only enforces *that* tests / lint run, not which ones.
- **Specific document formats** — ADR vs RFC vs design doc vs sketch. The workflow says *capture the decision*; the project picks the format.

If a law / persona / template you authored contains any of the above, it has leaked architecture / tooling prescription. Refactor toward process language ("decision recorded", "test evidence", "reviewer sign-off") and leave the format choice to the project.

### Validator checklist

Run these locally before activating a custom preset:

```bash
okt config validate <config-dir>/config/custom/<my-preset>.yaml
```

The validator rejects missing required config blocks, bad enum rows, invalid workflow references, unknown guard types, contradictory `mcp_commands` law rules, and command skills outside the persona repertoire. The field-level rules live in the configuration-guide modules above.

Warnings (non-fatal) flag template slug-vs-name mismatches and other low-severity drift; the runtime still loads but the agent may show a noisier prompt.

### Activating your preset

1. Drop the yaml in `<config-dir>/config/custom/<my-preset>.yaml` (preserves across `okt config defaults refresh`).
2. Set `.active` to `<my-preset>.yaml`:
   ```bash
   echo my-preset.yaml > <config-dir>/config/.active
   ```
3. The next CLI / TUI / MCP invocation resolves the new preset.

The TUI Settings › Config picker writes `.active` for you. The CLI accepts a per-invocation override via `--config <path>`.

### When to author vs fork

| Scenario | Action |
|---|---|
| Your team's process is close to omakase but adds one extra guard | Fork/import the workflow block and add the guard. |
| Your team likes omakase workflow but wants different role behavior | Keep the workflow, swap command bindings and personas. |
| Your team has a six-stage flow that does not match kaiseki's stages | Fork/import a workflow block, rename/reorder buckets, add/remove transitions. |
| Your team has a different mental model (e.g. Scrum sprints with retrospectives) | Author a new profile and split it into modules once it stabilizes. |

---

## See also

- [`command-surface.md`](./command-surface.md) — stable command tiers, roles, scopes, and write behavior.
- [`configuration-guide/README.md`](./configuration-guide/README.md) — modular YAML schema map.
- [`configuration-guide/workflows.md`](./configuration-guide/workflows.md) — workflow schema.
- [`configuration-guide/command-bindings.md`](./configuration-guide/command-bindings.md) — prompt binding schema.
- [`configuration-guide/guards.md`](./configuration-guide/guards.md) — guard types and their config.
- [`presets.md`](./presets.md) — preset discipline and workflow comparison.
- [`mcp.md`](./mcp.md) — MCP tool surface, prompt anatomy, tuning context cost.
- [`internal/data-model.md`](./internal/data-model.md) — SQLite schema and migration history.
- `internal/domain/event.go::KnownEventTypes` — canonical list of `events` payloads.
- [`why_omakiten.md`](./why_omakiten.md) — every cited work; per-preset "Methodology basis" anchors link here.
