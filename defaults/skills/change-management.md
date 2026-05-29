---
name: Change management
description: Approval matrix, sign-off discipline, audit-trail integrity, regulated-environment habits.
schema_version: 2
role_affinity:
  - Owner
  - Committer
  - Reviewer
---
Change management is the discipline of controlling how changes reach production so that each one is authorised, recorded, and reversible. It matters most in regulated environments (where ITIL change-management practice and controls such as those audited under SOC 2 / ISO 27001 apply), but the habits keep any system auditable.

## Approval matrix

Define, before the change, who must approve it based on its risk and scope. A low-risk reversible change may need only peer review; a change touching production data or a published contract needs a named, higher authority. The matrix removes ad-hoc judgement about *who can say yes* from the moment of pressure.

## Sign-off discipline

Approval is explicit and attributable, not implied by silence. Record who approved what, when, and on what basis. A change that proceeded without its required sign-off is a process failure even if the change itself was sound — the control exists to make authorisation provable after the fact.

## Audit-trail integrity

The trail of who requested, approved, deployed, and (if needed) rolled back a change must be complete and tamper-evident. Append-only records, immutable logs, and linked artifacts (the change, its approval, its deployment) are what let an auditor — or an incident responder — reconstruct exactly what happened.

## Regulated-environment habits

In regulated contexts, separation of duties (the author is not the sole approver), pre-approved standard changes for routine work, and a defined rollback plan per change are baseline expectations. Treat these as defaults, not overhead.

## Boundaries

Change management governs authorisation and record-keeping; it does not assess the technical correctness of the change — that is review and testing. Pair it with decision-records for the rationale and with risk-driven development for the irreversibility classification that calibrates how much control a change warrants.
