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

Each preset embodies a different process discipline level, but every preset runs the same underlying cycle: [PDCA — Plan-Do-Check-Act](./explanation/mental-models.md#pdca). Every action through Omakiten maps onto one of the four phases.

The eight `okt-*` commands map to PDCA phases:

| PDCA phase | `okt-*` command | What happens |
|---|---|---|
| **PLAN** | `okt-imagine` | Product-owner persona interrogates the user via [5W2H](./explanation/mental-models.md#5w2h). Define success in [SMART](./explanation/mental-models.md#smart) terms. Surface assumptions and gaps. Decide if the request is concrete enough to file. |
| **PLAN → DO** | `okt-create` | Formalize the imagined work as a task. [INVEST](./explanation/mental-models.md#invest) checklist on the user story. Acceptance criteria. Prioritization ([MoSCoW](./explanation/mental-models.md#moscow) or [RICE](./explanation/mental-models.md#rice)) when alternatives exist. Non-functional requirements named separately when relevant. |
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

- **Software engineering** (TBD / TDD / SRE / decision records) — how code lands.
- **Product management** ([5W2H](./explanation/mental-models.md#5w2h) / [SMART](./explanation/mental-models.md#smart) / [INVEST](./explanation/mental-models.md#invest) / [MoSCoW](./explanation/mental-models.md#moscow) / [RICE](./explanation/mental-models.md#rice) / outcomes) — what gets built and why.
- **Project management** ([PDCA](./explanation/mental-models.md#pdca) cycle awareness, staged delivery, audit trail) — how the work is structured and recorded.

A preset is **coherent** when its engineering rigor matches its product rigor matches its project-management rigor. izakaya keeps all three light (spike); omakase balances all three at mainstream professional level; kaiseki tightens all three with formal stages; shokunin elevates all three with audit-trail integrity and multi-reviewer sign-off.

---

## 🍻 izakaya — Lean spike, tracer-bullet, walking skeleton

> Hypothesis-driven exploration. Build only what proves the question. Kill, promote, or extend explicitly when the time-box runs out.

### Methodology basis

- **Lean Startup** — [Ries 2011](./reference/bibliography.md#ries-2011): build-measure-learn loops; MVP design.
- **Extreme Programming (XP) Spike** — [Beck & Andres 2004](./reference/bibliography.md#beck-andres-2004): time-boxed exploratory work, throwaway code, learning-first.
- **Tracer Bullet** — [Hunt & Thomas 1999](./reference/bibliography.md#hunt-thomas-1999) ch.7: thin end-to-end slice before depth.
- **Walking Skeleton** — [Cockburn — Crystal Clear](./reference/bibliography.md#cockburn-crystal): connect the wires first, deepen later.

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

### Persona, laws, skills, templates

<!-- BEGIN include:_generated/presets-izakaya.md -->
# Preset — Izakaya Workflow Preset

Auto-derived from `defaults/config/izakaya.yaml`.

<!-- SECTION:personas -->
## Personas

| Persona | Skills |
|---|---|
| `check-runner` | `test-driven-development`, `static-analysis-discipline`, `coverage-analysis`, `regression-detection`, `markdown` |
| `code-reviewer` | `refactoring-catalog`, `code-smells`, `solid-principles`, `legacy-seams`, `security-review-lens`, `markdown` |
| `commit-author` | `conventional-commits-spec`, `markdown` |
| `documentation-agent` | `documentation`, `architecture-mapping`, `readme-curation`, `markdown` |
| `tinkerer` | `lean-experimentation`, `tracer-bullet-shipping`, `time-box-discipline`, `markdown` |

<!-- END SECTION -->

<!-- SECTION:mcp-commands -->
## MCP command bindings

| Command | Persona | Laws (+/-) | Templates |
|---|---|---|---|
| `global` | — | +`template-fidelity`, +`authorize-remote-writes`, +`project-scope-only` | — |
| `okt` | tinkerer | — | — |
| `okt-check` | check-runner | +`time-boxed-check`, +`accept-yellow` | `comment-check-report` |
| `okt-commit` | commit-author | +`conventional-commits`, +`no-coauthored-by` | — |
| `okt-config` | documentation-agent | — | `config-orientation` |
| `okt-continue` | tinkerer | — | — |
| `okt-create` | tinkerer | +`hypothesis-required`, +`yagni-first` | `task-spike` |
| `okt-document` | documentation-agent | — | — |
| `okt-imagine` | tinkerer | -`template-fidelity` | — |
| `okt-implement` | tinkerer | +`time-boxed-spike`, +`tracer-bullet`, +`conventional-commits` | `pull-request` |
| `okt-resume` | tinkerer | — | — |
| `okt-review` | code-reviewer | +`time-boxed-review`, +`tracer-debt-acceptable` | `comment-review-findings`, `comment-refactor-opportunities` |

<!-- END SECTION -->

<!-- SECTION:workflow-guards -->
## Workflow guards

### `izakaya` workflow

**Transitions**

| From | To | Guards |
|---|---|---|
| `backlog` | `dev` | `#hypothesis`×1 · `wave_gate` |
| `dev` | `done` | — |
| `done` | `dev` | — |
| `dev` | `backlog` | — |
| `done` | `backlog` | — |

<!-- END SECTION -->
<!-- END include -->

Severity (`error` vs `warning`) for each law lives in [`_generated/entities-laws.md`](./_generated/entities-laws.md).

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
- **Continuous Integration** — [Fowler 2006](./reference/bibliography.md#fowler-ci-2006): green main as the source of truth.
- **DORA** — [Forsgren, Humble & Kim 2018](./reference/bibliography.md#forsgren-2018): lead time, deploy frequency, MTTR, change failure rate as the four optimization targets.
- **Test-Driven Development** — [Beck 2002](./reference/bibliography.md#beck-tdd-2002): red → green → refactor; tests-first on new behavior.
- **Conventional Commits** — [conventionalcommits.org](./reference/bibliography.md#conventional-commits): machine-parseable commit messages.
- **Boy Scout Rule** — [Martin — Clean Code 2008](./reference/bibliography.md#martin-clean-2008) p.14: leave code cleaner than you found it.

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
| dev → review | `comments_tagged: resume count=1` · `comments_tagged: tests-passing count=1` |
| review → done | `comments_tagged: documentation count=1` |
| regressions (6 paths) | — |

Operations: archive requires `#documentation`.

### Persona, laws, skills, templates

<!-- BEGIN include:_generated/presets-omakase.md -->
# Preset — Omakase Workflow Preset

Auto-derived from `defaults/config/omakase.yaml`.

<!-- SECTION:personas -->
## Personas

| Persona | Skills |
|---|---|
| `check-runner` | `test-driven-development`, `static-analysis-discipline`, `coverage-analysis`, `regression-detection`, `markdown` |
| `code-reviewer` | `refactoring-catalog`, `code-smells`, `solid-principles`, `legacy-seams`, `security-review-lens`, `markdown` |
| `commit-author` | `conventional-commits-spec`, `markdown` |
| `documentation-agent` | `documentation`, `architecture-mapping`, `requirements-mapping`, `readme-curation`, `markdown` |
| `engineer` | `trunk-based-development`, `continuous-integration`, `test-driven-development`, `dora-mindset`, `implementation`, `markdown` |
| `product-owner` | `discovery`, `user-story-writing`, `pdca-cycle`, `five-w-two-h`, `smart-goals`, `invest-stories`, `moscow-prioritization`, `rice-scoring`, `non-functional-requirements`, `markdown` |

<!-- END SECTION -->

<!-- SECTION:mcp-commands -->
## MCP command bindings

| Command | Persona | Laws (+/-) | Templates |
|---|---|---|---|
| `global` | — | +`template-fidelity`, +`authorize-remote-writes` | — |
| `okt` | engineer | — | — |
| `okt-check` | check-runner | +`findings-actionable`, +`severity-tagged` | `comment-check-report` |
| `okt-commit` | commit-author | +`conventional-commits`, +`no-coauthored-by`, +`scope-from-paths` | — |
| `okt-config` | documentation-agent | — | `config-orientation` |
| `okt-continue` | engineer | — | — |
| `okt-create` | product-owner | +`invest-stories`, +`outcome-over-output` | `user-story`, `task-bugfix`, `comment-smart-success`, `comment-moscow` |
| `okt-document` | documentation-agent | — | — |
| `okt-imagine` | product-owner | -`template-fidelity` | `comment-5w2h`, `comment-smart-success` |
| `okt-implement` | engineer | +`bounded-self-review`, +`no-silent-behavior-changes`, +`conventional-commits`, +`self-report`, +`green-main-always`, +`small-batches`, +`boy-scout-rule`, +`test-evidence` | `pull-request`, `comment-tests-passing`, `comment-refactor-drive-by` |
| `okt-resume` | engineer | — | — |
| `okt-review` | code-reviewer | +`findings-actionable`, +`no-praise-pad`, +`severity-tagged` | `comment-review-findings`, `comment-refactor-opportunities` |

<!-- END SECTION -->

<!-- SECTION:workflow-guards -->
## Workflow guards

### `omakase` workflow

**Operations**

| Operation | Guards |
|---|---|
| `archive` | `#documentation`×1 |

**Transitions**

| From | To | Guards |
|---|---|---|
| `backlog` | `dev` | `#self-branch`×1 · blockers in `done` · `wave_gate` |
| `dev` | `review` | `#resume`×1 · `#tests-passing`×1 |
| `review` | `done` | `#documentation`×1 |
| `dev` | `backlog` | — |
| `review` | `backlog` | — |
| `review` | `dev` | — |
| `done` | `review` | — |
| `done` | `dev` | — |
| `done` | `backlog` | — |

<!-- END SECTION -->
<!-- END include -->

Severity (`error` vs `warning`) for each law lives in [`_generated/entities-laws.md`](./_generated/entities-laws.md).

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

- **PMBOK Guide** — [PMI](./reference/bibliography.md#pmi-pmbok): stages, gates, sign-offs, change control.
- **Pressman** — [*Software Engineering: A Practitioner's Approach*](./reference/bibliography.md#pressman): staged lifecycle models.
- **Royce 1970** — [*Managing the Development of Large Software Systems*](./reference/bibliography.md#royce-1970): origin of waterfall + iterative refinement.
- **ISO/IEC 12207** — [software lifecycle processes](./reference/bibliography.md#iso-12207).

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
| dev → review | `comments_tagged: resume` · `comments_tagged: tests-passing` |
| review → docs | `comments_tagged: peer-review` |
| docs → done | `comments_tagged: documentation` |
| regressions | review→dev, docs→review, done→review, done→docs |

Operations: archive requires `#documentation`; delete requires `#peer-review`.

### Persona, laws, skills, templates

<!-- BEGIN include:_generated/presets-kaiseki.md -->
# Preset — Kaiseki Workflow Preset

Auto-derived from `defaults/config/kaiseki.yaml`.

<!-- SECTION:personas -->
## Personas

| Persona | Skills |
|---|---|
| `check-runner` | `test-driven-development`, `static-analysis-discipline`, `coverage-analysis`, `regression-detection`, `markdown` |
| `code-reviewer` | `refactoring-catalog`, `code-smells`, `solid-principles`, `legacy-seams`, `security-review-lens`, `markdown` |
| `commit-author` | `conventional-commits-spec`, `markdown` |
| `documentation-agent` | `documentation`, `architecture-mapping`, `requirements-mapping`, `readme-curation`, `markdown` |
| `methodical-engineer` | `staged-delivery`, `requirements-elicitation`, `design-documentation`, `decision-records`, `acceptance-criteria-writing`, `implementation`, `markdown` |
| `product-owner` | `discovery`, `user-story-writing`, `requirements-elicitation`, `acceptance-criteria-writing`, `pdca-cycle`, `five-w-two-h`, `smart-goals`, `invest-stories`, `moscow-prioritization`, `rice-scoring`, `non-functional-requirements`, `markdown` |

<!-- END SECTION -->

<!-- SECTION:mcp-commands -->
## MCP command bindings

| Command | Persona | Laws (+/-) | Templates |
|---|---|---|---|
| `global` | — | +`template-fidelity`, +`authorize-remote-writes`, +`project-scope-only` | — |
| `okt` | methodical-engineer | — | — |
| `okt-check` | check-runner | +`requirements-coverage-check`, +`decision-record-on-gap` | `comment-check-report` |
| `okt-commit` | commit-author | +`conventional-commits`, +`no-coauthored-by`, +`link-decision-record` | — |
| `okt-config` | documentation-agent | — | `config-orientation` |
| `okt-continue` | methodical-engineer | — | — |
| `okt-create` | product-owner | +`requirements-signed-off`, +`acceptance-criteria-required`, +`invest-stories`, +`outcome-over-output`, +`prioritization-recorded`, +`non-functional-explicit` | `task-feature`, `comment-requirements`, `comment-acceptance`, `comment-smart-success`, `comment-moscow`, `comment-rice-score`, `comment-non-functional` |
| `okt-document` | documentation-agent | — | — |
| `okt-imagine` | product-owner | -`template-fidelity` | `comment-5w2h`, `comment-smart-success` |
| `okt-implement` | methodical-engineer | +`design-recorded`, +`decision-record-on-divergence`, +`peer-review-required`, +`conventional-commits`, +`no-silent-behavior-changes` | `pull-request`, `decision-record`, `design-doc`, `comment-design-decision` |
| `okt-resume` | methodical-engineer | — | — |
| `okt-review` | code-reviewer | +`design-recorded-check`, +`decision-record-on-divergence` | `comment-review-findings`, `comment-refactor-opportunities` |

<!-- END SECTION -->

<!-- SECTION:workflow-guards -->
## Workflow guards

### `kaiseki` workflow

**Operations**

| Operation | Guards |
|---|---|
| `archive` | `#documentation`×1 |
| `delete` | `#peer-review`×1 |

**Transitions**

| From | To | Guards |
|---|---|---|
| `requirements` | `planning` | `#5w2h`×1 · `#requirements`×1 · `#acceptance`×1 |
| `planning` | `dev` | `#self-branch`×1 · `#design`×1 · blockers in `done`,`docs` · `wave_gate` |
| `dev` | `review` | `#resume`×1 · `#tests-passing`×1 |
| `review` | `docs` | `#peer-review`×1 |
| `docs` | `done` | `#documentation`×1 |
| `review` | `dev` | — |
| `docs` | `review` | — |
| `done` | `review` | — |
| `done` | `docs` | — |

<!-- END SECTION -->
<!-- END include -->

Severity (`error` vs `warning`) for each law lives in [`_generated/entities-laws.md`](./_generated/entities-laws.md).

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

- **Site Reliability Engineering** — [Beyer et al. 2016](./reference/bibliography.md#beyer-2016): SLI / SLO / error budgets, four golden signals.
- **Pre-mortem** — [Klein 2007](./reference/bibliography.md#klein-2007): imagine the failure before shipping.
- **Blameless Postmortems** — [Allspaw 2012](./reference/bibliography.md#allspaw-2012): no "human error" as root cause.
- **Continuous Delivery** — [Humble & Farley 2010](./reference/bibliography.md#humble-farley-2010): release gates, multi-reviewer sign-off, rollback-first design.

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
| dev → review | `comments_tagged: resume` · `comments_tagged: tests-passing` · `comments_tagged: rollback-plan` |
| review → docs | `comments_tagged: peer-review count=2` |
| docs → done | `comments_tagged: documentation` · `comments_tagged: lessons-learned` |
| regressions | review→dev, docs→review, done→review |

Operations: archive requires `#documentation` + `#lessons-learned`; delete requires `#peer-review`; unarchive requires `#peer-review`.

### Persona, laws, skills, templates

<!-- BEGIN include:_generated/presets-shokunin.md -->
# Preset — Shokunin Workflow Preset

Auto-derived from `defaults/config/shokunin.yaml`.

<!-- SECTION:personas -->
## Personas

| Persona | Skills |
|---|---|
| `check-runner` | `test-driven-development-strict`, `static-analysis-discipline`, `coverage-analysis`, `regression-detection`, `markdown` |
| `code-reviewer` | `refactoring-catalog`, `code-smells`, `solid-principles`, `legacy-seams`, `security-review-lens`, `markdown` |
| `commit-author` | `conventional-commits-spec`, `markdown` |
| `craftsperson` | `sre-discipline`, `risk-driven-development`, `postmortem-authoring`, `change-management`, `test-driven-development-strict`, `static-analysis-discipline`, `implementation`, `markdown` |
| `documentation-agent` | `documentation`, `architecture-mapping`, `requirements-mapping`, `readme-curation`, `markdown` |
| `product-owner` | `discovery`, `user-story-writing`, `pdca-cycle`, `five-w-two-h`, `smart-goals`, `invest-stories`, `moscow-prioritization`, `rice-scoring`, `okr-framing`, `non-functional-requirements`, `markdown` |

<!-- END SECTION -->

<!-- SECTION:mcp-commands -->
## MCP command bindings

| Command | Persona | Laws (+/-) | Templates |
|---|---|---|---|
| `global` | — | +`template-fidelity`, +`authorize-remote-writes`, +`project-scope-only` | — |
| `okt` | craftsperson | — | — |
| `okt-check` | check-runner | +`coverage-gate`, +`regression-required`, +`dual-signal-required` | `comment-check-report` |
| `okt-commit` | commit-author | +`conventional-commits`, +`no-coauthored-by`, +`link-task-comments` | — |
| `okt-config` | documentation-agent | — | `config-orientation` |
| `okt-continue` | craftsperson | — | — |
| `okt-create` | product-owner | +`blast-radius-awareness`, +`error-budget-aware`, +`invest-stories`, +`outcome-over-output`, +`prioritization-recorded`, +`non-functional-explicit` | `task-change-request`, `comment-requirements`, `comment-acceptance`, `comment-risk-assessment`, `comment-smart-success`, `comment-moscow`, `comment-rice-score`, `comment-okr`, `comment-non-functional` |
| `okt-document` | documentation-agent | +`blameless-postmortem` | `comment-postmortem`, `comment-lessons-learned` |
| `okt-imagine` | product-owner | -`template-fidelity` | `comment-5w2h`, `comment-smart-success` |
| `okt-implement` | craftsperson | +`pre-mortem-required`, +`rollback-plan-mandatory`, +`dual-peer-review`, +`coverage-gate`, +`blast-radius-awareness`, +`error-budget-aware`, +`conventional-commits`, +`no-silent-behavior-changes` | `pull-request`, `comment-pre-mortem`, `comment-rollback-plan`, `comment-peer-review-strict`, `comment-tests-passing-strict`, `comment-risk-assessment`, `comment-scribe-correction` |
| `okt-resume` | craftsperson | — | — |
| `okt-review` | code-reviewer | +`dual-review-required`, +`coverage-gate-check`, +`pre-mortem-aware` | `comment-review-findings`, `comment-refactor-opportunities` |

<!-- END SECTION -->

<!-- SECTION:workflow-guards -->
## Workflow guards

### `shokunin` workflow

**Operations**

| Operation | Guards |
|---|---|
| `archive` | `#documentation`×1 · `#lessons-learned`×1 |
| `delete` | `#peer-review`×1 |
| `unarchive` | `#peer-review`×1 |

**Transitions**

| From | To | Guards |
|---|---|---|
| `requirements` | `planning` | `#5w2h`×1 · `#requirements`×1 · `#acceptance`×1 |
| `planning` | `dev` | `#self-branch`×1 · `#pre-mortem`×1 · `#risk-assessment`×1 · blockers in `done`,`docs` · `wave_gate` |
| `dev` | `review` | `#resume`×1 · `#tests-passing`×1 · `#rollback-plan`×1 |
| `review` | `docs` | `#peer-review`×2 |
| `docs` | `done` | `#documentation`×1 · `#lessons-learned`×1 |
| `review` | `dev` | — |
| `docs` | `review` | — |
| `done` | `review` | — |

<!-- END SECTION -->
<!-- END include -->

Severity (`error` vs `warning`) for each law lives in [`_generated/entities-laws.md`](./_generated/entities-laws.md).

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

## Plans — multi-agent fan-out

Plans sit **on top of** the active workflow, not inside it. A plan groups child tasks into ordered **waves** so that two to four AI agents can fan out across the same goal without racing each other. The workflow shape (bucket cycle, transition guards, permissions) is unchanged — plans add a coordination layer.

### The wave-gate rule

Tasks inside the same wave run in parallel. Wave `N+1` is blocked until wave `N` is fully closed. The rule is enforced by the `wave_gate` guard (`internal/app/guards/evaluator.go:checkWaveGate`) registered alongside `blockers_in`, `comments_min`, and `comments_tagged` — see [Guards Guide § `wave_gate`](./guards-guide.md#wave_gate).

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
2. `SELECT` the next unblocked task in the active wave (no prior-wave pending, no dependency in flight).
3. `UPDATE tasks SET bucket_id=<dev>, assigned_to=<caller _agent_model>` in the same transaction.
4. Commit. Returns the claimed task or `{claimed: false}`.

Two concurrent calls land on the same write lock; the loser retries the SELECT and either claims a different task or returns empty. No double-claim is possible. The CLI handle (`okt plan claim <slug>`) hits the same primitive.

Agents should **never** call `tasks.move` to enter a plan task into `dev` — always go through `plans.claim_next`. Manual moves bypass the assignment write and leave the activity log inconsistent with the plan's progress view.

### Recovery from a crashed claim

`tasks.assigned_to` stays set if the claiming agent crashes mid-task. Recovery is deliberately human-driven:

- `okt assign <task_id>` (no `WHO`) clears the assignment.
- `okt move <task_id> backlog` clears it via the transition-out hook.

v1 does not auto-reclaim — silent reclaim would hide real-world agent failures. A v2 path is on the roadmap.

### Surfaces

- **MCP**: 7 tools under `plans.*` (`create`, `list`, `show`, `add_wave`, `assign_task`, `continue`, `claim_next`). See [MCP Guide § Plans](./mcp-guide.md#plans-wbs-style-multi-agent-orchestration).
- **CLI**: `okt plan create|list|show|wave-add|assign|claim` and the orthogonal `okt assign <task_id> [who]` for free-text assignment outside the plan flow. See [CLI Guide § Plans](./cli-guide.md#plans).
- **TUI**: a fourth sub-tab under `01 // TASKS` — list view first, then a column-per-wave network diagram per plan. See [TUI Guide § Tasks › Plans](./tui-guide.md#tasks--plans).
- **Search**: `plans.goal_body` is indexed in the unified FTS5 `search_index` so cross-project `search` finds plans by name or any phrase in the goal markdown.

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
- Unknown guard types (only `comments_tagged`, `comments_min`, `blockers_in`, `wave_gate`)
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

Every cited work lives in [`reference/bibliography.md`](./reference/bibliography.md) with a stable anchor per entry. Per-preset "Methodology basis" sections above link directly to those anchors.

### Omakiten reference docs

- [`configuration-guide.md`](configuration-guide.md) — every yaml field, semantics, validation rules.
- [`guards-guide.md`](guards-guide.md) — guard kinds, evaluation order, permissions resolution, operation guards.
- [`mcp-guide.md`](mcp-guide.md) — MCP tool surface, prompt anatomy, token costs.
- [`data-model-guide.md`](internal/data-model-guide.md) — SQLite schema and migration history.
- [`domain-events.md`](domain-events.md) — `events` table catalog and payload contracts.
