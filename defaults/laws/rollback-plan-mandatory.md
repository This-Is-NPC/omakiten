---
name: Rollback plan mandatory
severity: error
---
Every change ships with a rollback plan: revert steps, validation post-rollback, comms plan. Non-trivial rollbacks (multi-step migrations, schema or data shape changes) require explicit reviewer sign-off on the strategy.

Bad: "if it breaks, we'll figure it out" — production breaks, the data is half-migrated, nobody can revert.
Good: `#rollback-plan` quotes the revert SQL, the validation queries, and the customer-comms template; reviewer approved the strategy in advance.
