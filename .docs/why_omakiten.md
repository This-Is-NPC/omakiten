# Why Omakiten

## What Omakiten Is

Omakiten is a CLI/TUI for managing tasks, context, and rules for AI-assisted software development.

It acts as a local source of truth where humans and AI agents can check the current state of a project before continuing work.

Short description:

> Opinionated checkpoints for AI-driven development.

## Why The Name

Omakiten combines two ideas:

- Omakase: an opinionated, curated experience where good defaults reduce ambiguity.
- Kiten: a starting point, origin, or reference point.

Together, Omakiten means an opinionated point of origin for continuing development safely.

## The Problem

AI agents can lose context, assume outdated state, or take actions outside the intended workflow.

Traditional task managers are not designed to be a safety layer for agentic development. They usually store tasks, but they do not enforce workflow rules, guardrails, or structured context handoff.

Omakiten is designed to solve that gap.

## Core Ideas

- Source of truth: tasks, dependencies, workflow state, and context are stored locally and consistently.
- Checkpoint: humans and agents can resume from a known state.
- Guardrails: invalid workflow actions are blocked with clear errors. Per-bucket CRUD policy and operation guards apply to delete/archive too, not only to transitions.
- Memory: a unified FTS5 `search` index covers tasks, comments, errors, solutions, and plans across every project on the machine; project- and universal-scoped note-like content is indexed as comments, so agents stop re-discovering the same fix.
- Speaks your language: 21 bundled CLI/TUI language packs; CLI and TUI share the install-time picker, agent-output language is chosen separately, and all three are switchable later (`okt config language set`).
- Observable by design: every meaningful state change emits a typed domain event; a YAML hooks engine fires async actions and notification cards; `metrics.summary` benchmarks agent behaviour per model over a chosen window.
- Token economy: agent-facing output is structured, compact, and predictable; responses are token-counted against a budget you set so callers stay within context limits.
- Customization: workflows, laws, personas, skills, templates, themes, notifications, and language packs are shareable through YAML/Markdown — edit them, version them, copy a folder to a teammate.
- Local-first: every byte of state lives in a SQLite file under your home directory (or `.omakiten/` at the repo root). No account, no telemetry, no cloud.

## Positioning

Omakiten is not just a task manager.

It is a local checkpoint system for collaborative development with AI agents.

It is opinionated, but not closed. It provides strong defaults while keeping configuration portable and easy to share.

## Command Name

The suggested command is:

```bash
okt
```

Examples:

```bash
okt tui
okt list -b dev
okt move 42 --to done
okt mcp call search --input '{"query":"sqlite race","entity_types":["error","solution"]}'
okt mcp call metrics.summary --input '{"period":"30d"}'
```

---

# Mental models behind the presets

The frameworks every Omakiten preset assumes you have in your head. Each section has a stable anchor — link from `.docs/*.md` files as `[INVEST](./why_omakiten.md#invest)`.

## <a id="pdca"></a>PDCA — Plan-Do-Check-Act

The Shewhart ([1939](#shewhart-1939)) / Deming ([1986](#deming-1986)) quality-improvement loop, later carried into Total Quality Management and the Toyota Production System. Four phases repeated forever:

- **Plan** — define the problem, the hypothesis, and the measure of success.
- **Do** — execute the smallest experiment that can test the hypothesis.
- **Check** — compare actual against the success measure; analyse the gap.
- **Act** — adopt, abandon, or adjust based on what the data showed.

The `okt-*` command surface maps cleanly onto PDCA: `okt-shape` guides Plan by chaining granular discovery commands such as `okt-task-imagine`; `okt-task-create` plus the early phase of `okt-task-implement` are Do; the middle of `okt-task-implement` is Check; and the final review-and-promote sequence is Act.

PDCA is universally recognized, easy to teach, and gives users a stable mental map for every action they take through Omakiten regardless of preset.

## <a id="5w2h"></a>5W2H — Structured elicitation

Seven questions an interviewer asks before agreeing to file work:

| Question | Purpose |
|---|---|
| **What** | Concrete deliverable — observable behavior, not a vague theme. |
| **Why** | Outcome that justifies the work. Often the most-skipped question. |
| **Who** | User segment, persona, or stakeholder. Not "users" — be specific. |
| **When** | Deadline, dependency window, or "now/this quarter/next year". |
| **Where** | Surface, environment, or system affected. |
| **How** | Approach or constraint shaping execution. |
| **How much** | Budget — effort, money, or time-box. |

Used by the Owner role inside `okt-task-imagine` to surface gaps before work is filed. Vague answers ("the user", "soon", "important") are not accepted — the gap is named and the conversation continues.

## <a id="smart"></a>SMART — Success criteria

Doran ([1981](#doran-1981)). A success measure is SMART when it is:

- **Specific** — names the observable outcome, not a feeling.
- **Measurable** — can be checked against the system after the work lands.
- **Assignable** — somebody owns delivering it.
- **Realistic** — achievable inside the budget named in 5W2H.
- **Time-related** — anchored to a date, a sprint, or a milestone.

Recorded as a `#smart-success` comment during `okt-task-imagine`, then re-evaluated during the Check phase to decide whether the work is actually done.

## <a id="invest"></a>INVEST — User story quality

Wake ([2003](#wake-2003)). Six properties a user story needs:

- **Independent** — can be implemented without waiting on other stories in the same iteration.
- **Negotiable** — captures the user need, not the implementation contract; the team can still negotiate details.
- **Valuable** — delivers value to a real user, not just to the team.
- **Estimable** — small enough and clear enough that the team can size it.
- **Small** — fits inside one iteration with room left for review.
- **Testable** — comes with acceptance criteria the requester can verify.

Checked during `okt-task-create` after `okt-task-imagine` clarified what the work is.

## <a id="moscow"></a>MoSCoW — Prioritization (categorical)

Clegg & Barker ([1994](#clegg-barker-1994)). Four buckets when alternatives exist:

- **Must** — the release fails without it.
- **Should** — important; ship if at all possible, but the release survives a slip.
- **Could** — nice-to-have; ships only if the must/should set leaves room.
- **Won't (this time)** — explicitly out of scope; recorded so it doesn't drift back in mid-flight.

Recorded as a `#moscow` comment; cheap to apply when the comparison is qualitative.

## <a id="rice"></a>RICE — Prioritization (quantitative)

Intercom ([2017](#intercom-rice)). Score = (Reach × Impact × Confidence) ÷ Effort:

- **Reach** — number of users affected per unit time.
- **Impact** — qualitative scale (e.g. massive / high / medium / low / minimal mapped to 3 / 2 / 1 / 0.5 / 0.25).
- **Confidence** — 0–100% — how sure are we about the other numbers.
- **Effort** — person-weeks.

Recorded as a `#rice-score` comment when the team prefers a numeric ranking — typical in product backlog grooming sessions.

## <a id="okr"></a>OKR — Objectives & Key Results

Doerr ([2018](#doerr-2018)). Pairs an aspirational **Objective** (qualitative direction) with 2–5 **Key Results** (measurable outcomes that say whether the objective was met). Recorded as a `#okr` comment when the task contributes to a broader quarterly or annual goal — anchors individual work to the wider business outcome.

## How these compose

A typical `okt-task-imagine` → `okt-task-create` cycle uses several of these models together: 5W2H surfaces the problem, SMART pins down what success looks like, INVEST checks the resulting story, and MoSCoW or RICE (or both) sets priority against alternatives. PDCA frames the entire loop.

---

# References

Canonical citations for the models, laws, and disciplines above. Each entry has a stable anchor — link from any `.docs/*.md` file as `[short name](./why_omakiten.md#anchor)`.

### <a id="allspaw-2012"></a>Allspaw — Blameless PostMortems and a Just Culture (2012)
John Allspaw. *Blameless PostMortems and a Just Culture* (Etsy blog, 2012). Frames the postmortem as a system-of-systems learning loop rather than a fault hunt. Cited by shokunin's blameless-postmortem law.

### <a id="beck-andres-2004"></a>Beck & Andres — Extreme Programming Explained, 2nd ed. (2004)
Kent Beck and Cynthia Andres. *Extreme Programming Explained: Embrace Change*. Source for the time-boxed spike, TDD red-green-refactor, and the simplicity-first principle.

### <a id="beck-tdd-2002"></a>Beck — Test-Driven Development by Example (2002)
Kent Beck. *Test-Driven Development by Example*. Red → green → refactor as the discipline behind the omakase TDD law.

### <a id="beyer-2016"></a>Beyer et al. — Site Reliability Engineering (2016)
Betsy Beyer, Chris Jones, Jennifer Petoff, Niall Murphy (eds.). *Site Reliability Engineering* (O'Reilly, 2016). Source for the SRE discipline encoded in the shokunin preset: error budgets, change-management rigour, post-incident review.

### <a id="cagan-2017"></a>Cagan — Inspired (2017)
Marty Cagan. *Inspired: How to Create Tech Products Customers Love* (2nd ed., 2017). Outcome-over-output product framing.

### <a id="clegg-barker-1994"></a>Clegg & Barker — DSDM (1994)
Dai Clegg and Richard Barker. *Case Method Fast-Track: A RAD Approach* (Addison-Wesley, 1994). Origin of the MoSCoW prioritization model later codified in DSDM.

### <a id="cockburn-crystal"></a>Cockburn — Crystal Clear
Alistair Cockburn. *Crystal Clear: A Human-Powered Methodology for Small Teams* (2005). Source for the "walking skeleton" technique cited by izakaya.

### <a id="conventional-commits"></a>conventionalcommits.org — Conventional Commits spec
The Conventional Commits specification. Drives the `feat(scope): summary` discipline enforced by the omakase `conventional-commits` law.

### <a id="deming-1986"></a>Deming — Out of the Crisis (1986)
W. Edwards Deming. *Out of the Crisis*. Carried the Shewhart cycle into Total Quality Management and the Toyota Production System as the Plan-Do-Check-Act loop.

### <a id="doerr-2018"></a>Doerr — Measure What Matters (2018)
John Doerr. *Measure What Matters*. Canonical reference for the Objectives & Key Results (OKR) framing model.

### <a id="doran-1981"></a>Doran — SMART goals (1981)
George T. Doran. "There's a S.M.A.R.T. way to write management's goals and objectives". *Management Review*, 70(11), 1981. Original Specific / Measurable / Assignable / Realistic / Time-related framing.

### <a id="forsgren-2018"></a>Forsgren, Humble & Kim — Accelerate (2018)
Nicole Forsgren, Jez Humble, Gene Kim. *Accelerate: The Science of Lean Software and DevOps*. Source for the DORA four key metrics (lead time, deploy frequency, change failure rate, MTTR).

### <a id="fowler-ci-2006"></a>Fowler — Continuous Integration (2006)
Martin Fowler. "Continuous Integration", martinfowler.com (2006). Source for the omakase "green main always" law.

### <a id="humble-farley-2010"></a>Humble & Farley — Continuous Delivery (2010)
Jez Humble and David Farley. *Continuous Delivery*. Source for release gates, multi-reviewer sign-off, and rollback-first design referenced by shokunin.

### <a id="hunt-thomas-1999"></a>Hunt & Thomas — The Pragmatic Programmer (1999)
Andrew Hunt and David Thomas. *The Pragmatic Programmer*, ch.7 ("Tracer Bullets"). Origin of the tracer-bullet technique cited by izakaya.

### <a id="iso-12207"></a>ISO/IEC 12207 — Software lifecycle processes
International standard defining the software lifecycle process model. Referenced for context behind the staged-delivery framing of kaiseki.

### <a id="intercom-rice"></a>Intercom — RICE prioritization (2017)
Sean McBride (Intercom blog, 2017). Original write-up of the Reach × Impact × Confidence ÷ Effort scoring model.

### <a id="klein-2007"></a>Klein — Performing a Project Premortem (2007)
Gary Klein. "Performing a Project Premortem". *Harvard Business Review* (Sept 2007). Source for the pre-mortem ritual encoded in shokunin.

### <a id="martin-clean-2008"></a>Martin — Clean Code (2008)
Robert C. Martin. *Clean Code: A Handbook of Agile Software Craftsmanship*, p.14 ("Boy Scout Rule"). Source for the omakase `boy-scout-rule` law.

### <a id="nygard-2011"></a>Nygard — Documenting Architecture Decisions (2011)
Michael Nygard. "Documenting Architecture Decisions" (cognitect.com, 2011). Reference shape for architecture decision records (ADRs); kaiseki cites *the practice* without prescribing the exact ADR format.

### <a id="pmi-pmbok"></a>PMI — PMBOK Guide
Project Management Institute. *A Guide to the Project Management Body of Knowledge* (PMBOK Guide). Source for the staged delivery, gated sign-offs, and change-control framing of kaiseki.

### <a id="pressman"></a>Pressman — Software Engineering, A Practitioner's Approach
Roger Pressman. *Software Engineering: A Practitioner's Approach*. Staged lifecycle models referenced for context behind kaiseki.

### <a id="ries-2011"></a>Ries — The Lean Startup (2011)
Eric Ries. *The Lean Startup*. Source for the build-measure-learn loop and the hypothesis-required law referenced by izakaya.

### <a id="royce-1970"></a>Royce — Managing the Development of Large Software Systems (1970)
Winston Royce. "Managing the Development of Large Software Systems". Historical lineage for staged-delivery thinking.

### <a id="shewhart-1939"></a>Shewhart — Statistical Method From the Viewpoint of Quality Control (1939)
Walter A. Shewhart. *Statistical Method From the Viewpoint of Quality Control* (1939). Original three-step learning cycle that Deming extended into Plan-Do-Check-Act.

### <a id="wake-2003"></a>Wake — INVEST in Good Stories, INVEST in Better Stories (2003)
Bill Wake. "INVEST in Good Stories, INVEST in Better Stories" (xp123.com, 2003). Origin of the INVEST acronym (Independent / Negotiable / Valuable / Estimable / Small / Testable) for user-story quality.

---

## Update when

- A new mental model is wired into a preset and surfaced to users — add it to [Mental models behind the presets](#mental-models-behind-the-presets) with its tag and a primary citation.
- A new citation lands in any preset law, persona, or skill — append it to [References](#references) with a stable anchor.
- Positioning or product framing shifts (this is the canonical answer for "what is Omakiten?").

## See also

- [workflow.md](workflow.md) — concrete preset workflows that wire these models.
- [presets.md](presets.md) — side-by-side comparison of the four official presets.
