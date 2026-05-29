---
name: DORA mindset
description: Optimize for lead time, deploy frequency, MTTR, and change failure rate — small batches help all four.
schema_version: 2
role_affinity:
  - Owner
  - Builder
  - Committer
---
The DORA mindset (Forsgren, Humble & Kim, *Accelerate*, 2018, from the DevOps Research and Assessment program) measures software-delivery performance by four metrics that, together, balance speed against stability. Optimising one at the expense of another is the failure mode the four-metric set is designed to prevent.

## The four metrics

- **Lead time for changes** — time from commit to running in production. Shorter lead time means faster feedback and cheaper correction.
- **Deployment frequency** — how often the team ships to production. High frequency forces the automation and small batches that make delivery safe.
- **Mean time to restore (MTTR)** — how fast service is restored after an incident. Low MTTR reflects good observability, fast revert, and rehearsed recovery.
- **Change failure rate** — the proportion of changes that cause a failure in production. It is the stability counterweight to the two speed metrics.

## Small batches help all four

The central lever is batch size. Small changes deploy faster (lead time), more often (frequency), are quicker to diagnose and revert (MTTR), and carry less risk each (failure rate). A team that ships small, frequent, well-tested changes improves all four metrics at once — which is why batch size, not heroics, is the thing to optimise.

## Speed and stability together

The research finding worth internalising: speed and stability are not a trade-off in high-performing teams — they rise together, driven by automation, trunk-based flow, and small batches. Treat a metric pushed at another's expense as a warning, not a win.

## Boundaries

DORA measures the delivery system, not individuals; the metrics are diagnostic signals, not targets to game. Pair the mindset with the practices that move the metrics honestly — continuous integration, trunk-based development, and observability — rather than chasing the numbers directly.
