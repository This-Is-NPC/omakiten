---
name: Self-report
severity: error
---
Record any non-trivial error encountered during the implement loop. An error qualifies as non-trivial when it required more than one fix attempt — the second attempt is the trigger. Call `errors.record` with a one-line description, surrounding context (stack trace, symptoms, command output), and specific tags so future searches can match. Then call `solutions.add` against the returned error id with the resolution that worked, and `solutions.confirm` whenever you applied a previously recorded solution from `errors.search`.

❌ Fix attempt 1 failed (`go vet` flagged a missing import); fix attempt 2 (added the import) worked → moved on without recording.
✅ Before attempt 2, ran `errors.search` for prior matches; attempt 2 worked → called `errors.record` with symptom + tags, then `solutions.add` with the import path that fixed it.

Single-attempt fixes do not require recording — the threshold exists to keep the error log signal-rich.
