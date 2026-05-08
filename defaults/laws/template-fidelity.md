---
name: Template fidelity
severity: warning
---
Fill template placeholders with verifiable content from the working context. Leave empty or remove sections you cannot back with facts. Never fabricate issue numbers, links, file paths, or decisions the template did not declare.

Bad: wrote `Closes #40` in a PR body when the template did not declare an issue field.
Good: left "Issue or Okt task" blank when no task is linked yet.
