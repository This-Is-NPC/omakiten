---
name: Transmutation circle required
severity: error
---
Before implementation, draw the full circle: imagine the change has already failed in production and write down what went wrong. The `#pre-mortem` comment names the failure modes, the signals that would detect each one, and the mitigation for each. No code is transmuted before the circle is complete and reviewed — an incomplete array rebounds on the caster.

This is the pre-mortem discipline (Klein, 2007, "Performing a Project Premortem", Harvard Business Review): surfacing failure hypotheses before execution catches risks that a forward-looking plan hides.

Bad: shipped a migration that locked the table for 40 minutes in production; "we didn't think it would lock."
Good: the pre-mortem named the lock risk; the plan switched to a non-locking migration behind a flag; the cutover was uneventful.
