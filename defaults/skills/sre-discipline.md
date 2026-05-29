---
name: SRE discipline
description: SLI / SLO / error-budget thinking; four golden signals (latency, traffic, errors, saturation).
schema_version: 2
role_affinity:
  - Owner
  - Builder
  - Reviewer
---
Site Reliability Engineering (Beyer et al., *Site Reliability Engineering*, Google, 2016) makes reliability an explicit, measured engineering target rather than a vague aspiration. Its core instruments are service-level indicators, objectives, and the error budget they imply.

## SLI → SLO → error budget

- **SLI (Service Level Indicator)** — a measured quantity of service health: request success rate, p99 latency, availability. It must be something the system actually emits and a user actually feels.
- **SLO (Service Level Objective)** — the target value for an SLI over a window (e.g. 99.9% of requests succeed over 30 days). The SLO is a deliberate choice, not an aspiration to 100%.
- **Error budget** — the complement of the SLO (a 99.9% SLO permits 0.1% failure). The budget is spendable: while it remains, the team can ship and take risk; when it is exhausted, reliability work takes priority over features. This converts the reliability-versus-velocity argument into a number.

## Four golden signals

Monitor every user-facing system on four signals:

- **Latency** — how long requests take, split by success and error (a fast error is still a problem).
- **Traffic** — demand on the system, in a service-relevant unit.
- **Errors** — the rate of failed requests, including soft failures.
- **Saturation** — how full the most constrained resource is; the leading indicator of imminent trouble.

These four cover most operational questions without drowning in metrics.

## Boundaries

SRE discipline sets and measures reliability targets; it does not specify the application's features. Pair it with the DORA mindset (which measures delivery) and with postmortem authoring (which closes the loop after the error budget is spent on a real incident).
