---
name: PDCA cycle
description: Plan-Do-Check-Act awareness — recognize which phase each okt-* command represents and name it for the user.
schema_version: 2
role_affinity:
  - Concierge
  - Owner
---
The PDCA cycle — Plan, Do, Check, Act — is Deming's continuous-improvement loop (Deming, *Out of the Crisis*, 1986, building on Shewhart). It frames work as an iterative loop rather than a one-shot pass, and its value here is orientation: recognising which phase a given action sits in and naming that phase for the user so they always know where they are in the loop.

## The four phases

- **Plan** — establish the objective and the approach. Requirements, planning, and design-documentation activity live here. The question is *what are we about to do and how will we know it worked?*
- **Do** — execute the plan, ideally as a small controlled increment. Implementation and the building work sit here.
- **Check** — compare the result against the expectation set in Plan. Review, testing, and verification belong here; the question is *did it do what we predicted?*
- **Act** — respond to the gap: standardise what worked, correct what did not, and feed the learning into the next Plan. Postmortems, retrospectives, and decision records close the loop.

## Naming the phase

When guiding a user through the workflow, name the phase the current command represents ("this is the Check step — we are verifying against the acceptance criteria"). The orientation keeps the loop visible and makes the next phase obvious, which is the whole point of a cycle over a checklist.

## Boundaries

PDCA is an awareness frame, not a deliverable. It does not replace the disciplines inside each phase (elicitation, TDD, review) — it situates them. Use it to give the user a map, not to add ceremony.
