# Mental Models

The frameworks every Omakiten preset assumes you have in your head. Each section has a stable anchor — link from `.docs/*.md` files as `[INVEST](./explanation/mental-models.md#invest)`, from nested docs as `[INVEST](../explanation/mental-models.md#invest)`, or from this folder as `[INVEST](./mental-models.md#invest)`.

## <a id="pdca"></a>PDCA — Plan-Do-Check-Act

The Shewhart ([1939](../reference/bibliography.md#shewhart-1939)) / Deming ([1986](../reference/bibliography.md#deming-1986)) quality-improvement loop, later carried into Total Quality Management and the Toyota Production System. Four phases repeated forever:

- **Plan** — define the problem, the hypothesis, and the measure of success.
- **Do** — execute the smallest experiment that can test the hypothesis.
- **Check** — compare actual against the success measure; analyse the gap.
- **Act** — adopt, abandon, or adjust based on what the data showed.

The `okt-*` command surface maps cleanly onto PDCA: `okt-imagine` runs Plan, `okt-create` plus the early phase of `okt-implement` are Do, the middle of `okt-implement` is Check, and the final review-and-promote sequence is Act.

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

Used by the `product-owner` persona inside `okt-imagine` to surface gaps before work is filed. Vague answers ("the user", "soon", "important") are not accepted — the gap is named and the conversation continues.

## <a id="smart"></a>SMART — Success criteria

Doran ([1981](../reference/bibliography.md#doran-1981)). A success measure is SMART when it is:

- **Specific** — names the observable outcome, not a feeling.
- **Measurable** — can be checked against the system after the work lands.
- **Assignable** — somebody owns delivering it.
- **Realistic** — achievable inside the budget named in 5W2H.
- **Time-related** — anchored to a date, a sprint, or a milestone.

Recorded as a `#smart-success` comment during `okt-imagine`, then re-evaluated during the Check phase to decide whether the work is actually done.

## <a id="invest"></a>INVEST — User story quality

Wake ([2003](../reference/bibliography.md#wake-2003)). Six properties a user story needs:

- **Independent** — can be implemented without waiting on other stories in the same iteration.
- **Negotiable** — captures the user need, not the implementation contract; the team can still negotiate details.
- **Valuable** — delivers value to a real user, not just to the team.
- **Estimable** — small enough and clear enough that the team can size it.
- **Small** — fits inside one iteration with room left for review.
- **Testable** — comes with acceptance criteria the requester can verify.

Checked during `okt-create` after `okt-imagine` clarified what the work is.

## <a id="moscow"></a>MoSCoW — Prioritization (categorical)

Clegg & Barker ([1994](../reference/bibliography.md#clegg-barker-1994)). Four buckets when alternatives exist:

- **Must** — the release fails without it.
- **Should** — important; ship if at all possible, but the release survives a slip.
- **Could** — nice-to-have; ships only if the must/should set leaves room.
- **Won't (this time)** — explicitly out of scope; recorded so it doesn't drift back in mid-flight.

Recorded as a `#moscow` comment; cheap to apply when the comparison is qualitative.

## <a id="rice"></a>RICE — Prioritization (quantitative)

Intercom ([2017](../reference/bibliography.md#intercom-rice)). Score = (Reach × Impact × Confidence) ÷ Effort:

- **Reach** — number of users affected per unit time.
- **Impact** — qualitative scale (e.g. massive / high / medium / low / minimal mapped to 3 / 2 / 1 / 0.5 / 0.25).
- **Confidence** — 0–100% — how sure are we about the other numbers.
- **Effort** — person-weeks.

Recorded as a `#rice-score` comment when the team prefers a numeric ranking — typical in product backlog grooming sessions.

## <a id="okr"></a>OKR — Objectives & Key Results

Doerr ([2018](../reference/bibliography.md#doerr-2018)). Pairs an aspirational **Objective** (qualitative direction) with 2–5 **Key Results** (measurable outcomes that say whether the objective was met). Recorded as a `#okr` comment when the task contributes to a broader quarterly or annual goal — anchors individual work to the wider business outcome.

## How these compose

A typical `okt-imagine` → `okt-create` cycle uses several of these models together: 5W2H surfaces the problem, SMART pins down what success looks like, INVEST checks the resulting story, and MoSCoW or RICE (or both) sets priority against alternatives. PDCA frames the entire loop.
