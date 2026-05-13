# Workflow Guide

Every team works differently, and any one preset will feel wrong to half its users. Omakiten ships four official workflow presets and a path for users to author their own. Each preset embodies a documented software-engineering process — peer review, decision records, test evidence, audit trail, lessons learned — without prescribing architectural patterns. Whether the project is hexagonal, layered, MVC, event-driven, or something else, the workflow stays the same.

This guide is the authoritative reference for picking a preset, understanding what it enforces, and forking your own.

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

Each preset embodies a different process discipline level, but every preset runs the same underlying cycle: **Plan-Do-Check-Act**, the Shewhart (1939) / Deming quality loop that anchors Total Quality Management and the Toyota Production System. PDCA is universally recognized, easy to teach, and gives users a mental map for every action they take through Omakiten.

The eight `okt-*` commands map to PDCA phases:

| PDCA phase | `okt-*` command | What happens |
|---|---|---|
| **PLAN** | `okt-imagine` | Product-owner persona interrogates the user via 5W2H (What / Why / Who / When / Where / How / How much). Define success in SMART terms. Surface assumptions and gaps. Decide if the request is concrete enough to file. |
| **PLAN → DO** | `okt-create` | Formalize the imagined work as a task. INVEST checklist on the user story. Acceptance criteria. Prioritization (MoSCoW or RICE) when alternatives exist. Non-functional requirements named separately when relevant. |
| **DO** | task in `dev`, `okt-continue`, early `okt-implement` | Execute the planned increment. Test-first, conventional commits, small batches (the engineering discipline each preset enforces). |
| **ACT** | mid `okt-implement` | Adjust during execution — drive-by cleanup, decision records on divergence, refactors, escalate when guards block. |
| **CHECK** | end of `okt-implement` → task in `review` → `done` | Verify the outcome against the SMART success metric defined in PLAN. Peer review. Tests passing. Promote to `done` only when the loop closes. |

`okt-document` and `okt-config` sit outside the main loop — they orient the agent (CHECK-flavored) and read-only.

### What each phase produces

| Phase | Artifacts a user sees at the end of the phase |
|---|---|
| PLAN | `#5w2h` comment, `#acceptance` comment, `#smart-success` comment (or `#hypothesis` in izakaya). Task body filled per the chosen `task-*` template. |
| DO | Code commits (conventional format), branch tagged via `#self-branch` comment, `#design` comment (kaiseki) or `#pre-mortem` + `#risk-assessment` (shokunin). |
| ACT | Decision records, drive-by refactor comments, ADR-style files at `docs/decisions/` (or the project's preferred location). |
| CHECK | `#resume` comment, `#tests-passing` comment with evidence, `#peer-review` comment(s), `#documentation` comment, `#lessons-learned` comment (shokunin). |

### Cross-discipline coherence

Three disciplines ride together at the preset's chosen level:

- **Software engineering** (TBD / TDD / SRE / decision records — task #97's contribution) — how code lands.
- **Product management** (5W2H / SMART / INVEST / MoSCoW / RICE / outcomes — this discipline) — what gets built and why.
- **Project management** (PDCA cycle awareness, staged delivery, audit trail) — how the work is structured and recorded.

A preset is **coherent** when its engineering rigor matches its product rigor matches its project-management rigor. izakaya keeps all three light (spike); omakase balances all three at mainstream professional level; kaiseki tightens all three with formal stages; shokunin elevates all three with audit-trail integrity and multi-reviewer sign-off.

---

## 🍻 izakaya — Lean spike, tracer-bullet, walking skeleton

> Hypothesis-driven exploration. Build only what proves the question. Kill, promote, or extend explicitly when the time-box runs out.

### Methodology basis

- **Lean Startup** — Ries 2011: build-measure-learn loops; MVP design.
- **Extreme Programming (XP) Spike** — Beck/Andres 2004: time-boxed exploratory work, throwaway code, learning-first.
- **Tracer Bullet** — Hunt/Thomas 1999, *Pragmatic Programmer* ch.7: thin end-to-end slice before depth.
- **Walking Skeleton** — Cockburn, *Crystal Clear*: connect the wires first, deepen later.

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
| backlog → dev | `comments_tagged: hypothesis count=1` |
| dev → done | — |
| dev → backlog | — |
| done → dev | — |
| done → backlog | — |

Operations: no guards (archive / delete / unarchive free).

### Persona, laws, skills, templates

- **Persona**: `tinkerer`
- **Laws**: `hypothesis-required` (error), `time-boxed-spike` (error), `yagni-first` (warning), `tracer-bullet` (warning), plus the shared globals (`template-fidelity`, `authorize-remote-writes`, `project-scope-only`).
- **Skills**: `lean-experimentation`, `tracer-bullet-shipping`, `time-box-discipline`, `markdown`.
- **Templates**: `task-spike`, `comment-hypothesis`, `comment-discard`, `comment-promote`. Falls back to the shared `pull-request`, `comment-resume`, and `config-orientation`.

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

- **Trunk-Based Development** — short-lived branches, fast revert, feature flags. paulhammant.com TBD playbook.
- **Continuous Integration** — Fowler 2006: green main as the source of truth.
- **DORA** — Forsgren/Humble/Kim *Accelerate* 2018: lead time, deploy frequency, MTTR, change failure rate as the four optimization targets.
- **Test-Driven Development** — Beck: red → green → refactor; tests-first on new behavior.
- **Conventional Commits** — conventionalcommits.org: machine-parseable commit messages.
- **Boy Scout Rule** — Martin *Clean Code* 2008 p.14: leave code cleaner than you found it.

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
| backlog → dev | `comments_tagged: self-branch count=1` · `blockers_in: [done]` |
| dev → review | `comments_tagged: resume count=1` · `comments_tagged: tests-passing count=1` |
| review → done | `comments_tagged: documentation count=1` |
| regressions (6 paths) | — |

Operations: archive requires `#documentation`.

### Persona, laws, skills, templates

- **Persona**: `engineer` (TBD/DORA voice).
- **Laws**: `green-main-always` (error), `test-evidence` (error), `small-batches` (warning), `boy-scout-rule` (warning) plus the shared `conventional-commits`, `bounded-self-review`, `no-silent-behavior-changes`, `self-report`, and the globals.
- **Skills**: `trunk-based-development`, `continuous-integration`, `test-driven-development`, `dora-mindset`, `implementation`, `markdown`.
- **Templates**: `pull-request`, `comment-resume`, `comment-selfbranch`, `comment-documentation`, `comment-tests-passing`, `comment-refactor-drive-by`, `task-bugfix`, `user-story`.

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

- **PMBOK Guide** — PMI: stages, gates, sign-offs, change control.
- **Pressman** — *Software Engineering: A Practitioner's Approach*: staged lifecycle models.
- **Royce 1970** — *Managing the Development of Large Software Systems*: origin of waterfall + iterative refinement.
- **ISO/IEC 12207** — software lifecycle processes.

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
| requirements → planning | `comments_tagged: requirements` · `comments_tagged: acceptance` |
| planning → dev | `comments_tagged: self-branch` · `comments_tagged: design` · `blockers_in: [done, docs]` |
| dev → review | `comments_tagged: resume` · `comments_tagged: tests-passing` |
| review → docs | `comments_tagged: peer-review` |
| docs → done | `comments_tagged: documentation` |
| regressions | review→dev, docs→review, done→review, done→docs |

Operations: archive requires `#documentation`; delete requires `#peer-review`.

### Persona, laws, skills, templates

- **Persona**: `methodical-engineer` (staged-delivery voice; no architecture prescription).
- **Laws**: `requirements-signed-off` (error), `design-recorded` (error), `decision-record-on-divergence` (error), `acceptance-criteria-required` (error), `peer-review-required` (error), plus shared globals + `conventional-commits` + `no-silent-behavior-changes`.
- **Skills**: `requirements-elicitation`, `design-documentation`, `decision-records`, `acceptance-criteria-writing`, `staged-delivery`, `implementation`, `markdown`.
- **Templates**: `task-feature`, `decision-record`, `design-doc`, `comment-requirements`, `comment-acceptance`, `comment-peer-review`, `comment-design-decision`, plus shared `pull-request`, `comment-resume`, `comment-documentation`.

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

- **Site Reliability Engineering** — Beyer/Jones/Petoff/Murphy, *Site Reliability Engineering* (Google, 2016): SLI / SLO / error budgets, four golden signals.
- **Pre-mortem** — Klein, *Performing a Project Premortem*, HBR 2007: imagine the failure before shipping.
- **Blameless Postmortems** — Allspaw, Etsy 2012: no "human error" as root cause.
- **Continuous Delivery** — Humble/Farley 2010: release gates, multi-reviewer sign-off, rollback-first design.

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
| requirements → planning | `comments_tagged: requirements` · `comments_tagged: acceptance` |
| planning → dev | `comments_tagged: self-branch` · `comments_tagged: pre-mortem` · `comments_tagged: risk-assessment` · `blockers_in: [done, docs]` |
| dev → review | `comments_tagged: resume` · `comments_tagged: tests-passing` · `comments_tagged: rollback-plan` |
| review → docs | `comments_tagged: peer-review count=2` |
| docs → done | `comments_tagged: documentation` · `comments_tagged: lessons-learned` |
| regressions | review→dev, docs→review, done→review |

Operations: archive requires `#documentation` + `#lessons-learned`; delete requires `#peer-review`; unarchive requires `#peer-review`.

### Persona, laws, skills, templates

- **Persona**: `craftsperson`.
- **Laws**: `pre-mortem-required` (error), `rollback-plan-mandatory` (error), `dual-peer-review` (error), `coverage-gate` (error), `blameless-postmortem` (error), `audit-trail-integrity` (error), `error-budget-aware` (warning), `blast-radius-awareness` (warning), plus shared globals + `conventional-commits` + `no-silent-behavior-changes`.
- **Skills**: `sre-discipline`, `risk-driven-development`, `postmortem-authoring`, `change-management`, `test-driven-development-strict`, `static-analysis-discipline`, `implementation`, `markdown`.
- **Templates**: `task-change-request`, `comment-pre-mortem`, `comment-rollback-plan`, `comment-peer-review-strict`, `comment-tests-passing-strict`, `comment-postmortem`, `comment-lessons-learned`, `comment-risk-assessment`, `comment-scribe-correction`, plus shared `pull-request`, `comment-resume`, `comment-documentation`.

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

## Authoring your own preset

The four official presets are starting points. Forking is the expected way to make a preset fit your team.

### Naming convention

| Entity | Pattern | Example |
|---|---|---|
| Persona file | `defaults/personas/<slug>.md` — single noun, kebab-case | `tinkerer`, `methodical-engineer` |
| Skill file | `defaults/skills/<slug>.md` — noun or verb-noun | `lean-experimentation`, `staged-delivery` |
| Law file (shared body) | `defaults/laws/<slug>.md` — descriptive phrase | `template-fidelity`, `green-main-always` |
| Law file (preset-specific) | `defaults/laws/<slug>.md` — unique phrase, no preset prefix | `yagni-first`, `pre-mortem-required` |
| Template (shared body) | `defaults/templates/<kind>-<purpose>.md` | `comment-resume`, `pull-request` |
| Template (preset-specific, kind unique) | `<kind>-<purpose>.md` | `comment-hypothesis`, `comment-postmortem` |
| Template (preset-specific, kind collides) | `<kind>-<purpose>-<suffix>.md` | `comment-tests-passing` vs `comment-tests-passing-strict` |
| Workflow yaml | `defaults/config/<preset>.yaml` (canonical) or `<config-dir>/config/custom/<preset>.yaml` (user) | `omakase.yaml`, `my-omakase.yaml` |
| Tag name in guards | single kebab-case word, no `#` prefix in YAML | `self-branch`, `tests-passing`, `peer-review` |

### Token-friendly principles

Resolved MCP prompts ship inline with every command call. Every byte counts.

| Constraint | Reason |
|---|---|
| Law body ≤ 120 tokens | Laws ship inline; keep them compact |
| Skill body ≤ 80 tokens | Skill names ship inline; bodies are for `templates.show` style on-demand reads |
| Persona body ≤ 200 tokens | Persona is read every turn |
| Template body ≤ 250 tokens (frontmatter description ≤ 110 chars) | Template body is fetched JIT via `templates.show` |
| Law `Bad:` / `Good:` examples only on judgment-call laws | Examples cost tokens; reserve them for clarification |
| Industry abbreviations inline (TBD, DORA, TDD, SRE, SLO, ADR) | Definitions live once in the persona / guide; don't repeat |

### Reuse rule

Reuse a persona / law / skill / template across presets **only when its body is identical**. If a preset needs a stricter variant, create a new entity with a distinct slug — do not parameterize via flags or includes. The runtime distinguishes intent via slug.

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

The validator rejects:

- Missing required config blocks (mcp / views / priorities / severities / etc.)
- Dangling refs in `mcp_commands` (persona / law / template slug not found)
- Unknown guard types (only `comments_tagged`, `comments_min`, `blockers_in`)
- Permissions referencing buckets that do not exist
- Duplicate ids / values in priorities / severities / buckets

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
| Your team's process is close to omakase but adds one extra guard | Fork omakase, add the guard, keep the rest |
| Your team writes ADRs but kaiseki's `comment-design-decision` references "decision record" generically | Fork kaiseki, update the template wording — no need to invent a new preset |
| Your team has a six-stage flow that does not match kaiseki's stages | Fork kaiseki, rename / reorder buckets, add / remove transitions |
| Your team has a completely different mental model (e.g. Scrum sprints with retrospectives) | Author a new preset from scratch following the naming convention |

---

## References

### Methodology sources

- Ries, Eric. *The Lean Startup* (2011).
- Beck, Kent and Cynthia Andres. *Extreme Programming Explained* (2nd ed., 2004).
- Hunt, Andrew and David Thomas. *The Pragmatic Programmer* (1999), ch.7 "Tracer Bullets".
- Cockburn, Alistair. *Crystal Clear* — walking skeleton; *Hexagonal Architecture* (2005) — referenced for context only, not enforced.
- Fowler, Martin. "Continuous Integration" (2006).
- Forsgren, Nicole, Jez Humble, Gene Kim. *Accelerate* (2018) — DORA metrics.
- Martin, Robert C. *Clean Code* (2008) p.14 — Boy Scout rule.
- PMI. *PMBOK Guide* — staged delivery, sign-offs, change control.
- Pressman, Roger. *Software Engineering: A Practitioner's Approach* — staged lifecycle models.
- Royce, Winston. "Managing the Development of Large Software Systems" (1970).
- ISO/IEC 12207 — software lifecycle processes.
- Beyer, Betsy et al. *Site Reliability Engineering* (Google, 2016).
- Klein, Gary. "Performing a Project Premortem", *Harvard Business Review* (2007).
- Allspaw, John. "Blameless PostMortems and a Just Culture" (Etsy blog, 2012).
- Humble, Jez and David Farley. *Continuous Delivery* (2010).
- conventionalcommits.org — Conventional Commits spec.

### Omakiten reference docs

- [`configuration-guide.md`](configuration-guide.md) — every yaml field, semantics, validation rules.
- [`guards-guide.md`](guards-guide.md) — guard kinds, evaluation order, permissions resolution, operation guards.
- [`mcp-guide.md`](mcp-guide.md) — MCP tool surface, prompt anatomy, token costs.
- [`data-model-guide.md`](data-model-guide.md) — SQLite schema and migration history.
- [`domain-events.md`](domain-events.md) — `events` table catalog and payload contracts.
