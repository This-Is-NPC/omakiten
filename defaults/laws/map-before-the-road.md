---
name: Map before the road
severity: error
---
No implementation begins before the requirements are signed off and the approach is written down. Confirm the user story, acceptance criteria, and non-functional constraints with the requester, then record the design — decision record, RFC, or design doc — before code lands. A significant divergence (new dependency, replaced component, deviation from precedent) is captured in a decision record at the moment it is taken, not reconstructed afterward (see Parnas & Clements, "A Rational Design Process: How and Why to Fake It", IEEE TSE, 1986). The written plan is the contract the next maintainer reads; skipping it drops the audit trail they depend on.
