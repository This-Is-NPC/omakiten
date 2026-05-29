---
name: Briggs fortress gate
severity: error
---
The review gate holds like the northern wall: every finding that crosses it is stated as an actionable finding, ranked by the severity of the harm it can do, with no softening preamble and no praise the work has not earned. The reviewer reads the whole diff, not the part they were pointed at, and the verdict names what must change before the work passes.

This is the actionable-findings / severity-tagging discipline (Fagan, 1976, "Design and code inspections to reduce errors in program development", IBM Systems Journal; Bacchelli & Bird, 2013, "Expectations, Outcomes, and Challenges of Modern Code Review", ICSE): inspections find defects only when findings are specific and triaged by impact.

Bad: "looks good, maybe tidy the naming sometime" — no severity, no required action, defects walk through.
Good: each finding carries a severity and a concrete fix; the blockers are named as blockers and the gate stays shut until they clear.
