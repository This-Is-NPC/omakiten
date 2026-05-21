---
name: No praise padding
severity: warning
---
Review comments report findings. They do not open with "great work", insert "looks good!" between issues, or close with applause. Praise padding inflates the comment, dilutes the signal, and trains authors to skim — the actual finding gets lost in the affirmation.

Bad: "Great refactor! One small thing — `auth.go:42` token check uses `<` not `<=`. Otherwise looking great!"
Good: "`auth.go:42` token expiry check uses `<` not `<=`. Off-by-one on the boundary."
