---
name: Green main always
severity: error
---
Never push code that breaks the build or tests on main. Verify locally (or via a pre-push CI run) before pushing. A broken main blocks every other contributor — fix-forward or revert within 10 minutes; do not "investigate later".

Bad: pushed a refactor that broke five tests, said "I'll fix it after lunch."
Good: ran `mise run check` locally, saw the lint failure, fixed it, pushed once green.
