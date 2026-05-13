---
name: Boy Scout rule
severity: warning
---
Leave code cleaner than you found it. Opportunistic small refactors during feature work are encouraged when they touch the affected area. Document each drive-by cleanup in a `#refactor-drive-by` comment; assert no behavior change.

Bad: rewrote an unrelated module "while I was there" without a comment; reviewer can't tell what is feature vs cleanup.
Good: renamed two unclear variables in the file you were editing, dropped a #refactor-drive-by noting the change has no behavioral effect.
