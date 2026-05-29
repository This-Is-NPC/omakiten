---
name: Postmortem authoring
description: Blameless 5-whys, timeline reconstruction (UTC), action items with owners and due dates.
schema_version: 2
role_affinity:
  - Scribe
  - Owner
  - Reviewer
---
A postmortem turns an incident into durable learning. The standard is blameless (Google SRE practice; Allspaw, *Blameless PostMortems*, 2012): the document explains how the system and process allowed the failure, not who to blame, because blame suppresses the honest disclosure that prevents recurrence.

## Blameless framing

Write about systems and decisions, not people. Replace "X deployed the bad config" with "the deploy pipeline accepted a config that no gate validated." The goal is a culture where engineers report what they did freely, which only holds if the document never punishes them for it.

## Timeline reconstruction

Reconstruct the sequence of events in a single timezone — UTC — to avoid the off-by-hours errors that creep in across regions. Record detection, escalation, mitigation, and resolution timestamps from the logs and chat record rather than memory. The timeline is the factual spine; analysis hangs off it.

## Five whys

Drive to the contributing causes with iterative *why* questioning (the Toyota "5 Whys", Ohno), but treat it as a tool for finding a *chain* of causes, not a single root — most incidents have several. Stop when the next *why* leaves the system you can actually change.

## Action items

Every contributing cause yields at least one action item, and every action item has an explicit owner and a due date. An action item without an owner is a wish; without a due date it is indefinitely deferred. Track them to completion — the postmortem's value is realised only when the actions land.

## Boundaries

The postmortem analyses and prevents recurrence; it is not an incident-response runbook (that is the live procedure) nor a performance review. Pair it with SRE discipline (which defines the reliability targets the incident breached) and with decision-records for any lasting architectural change the actions produce.
