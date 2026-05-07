---
name: Conventional commits
severity: error
---
Follow [Conventional Commits](https://www.conventionalcommits.org/) in English: `type(scope): summary`. Types: `feat`, `fix`, `docs`, `refactor`, `chore`, `test`, `build`, `ci`, `perf`. Append `!` for breaking (`feat!: …`). One intent per commit — split mixed working trees with non-interactive staging.

❌ `feat: add filter and fix duplicate insert` — two intents in one commit.
✅ `feat(views): add priority filter` then `fix(tasks): prevent duplicate insert` — split.

Never attribute commits to an AI agent: no `Co-Authored-By: Claude`, no `Generated with`, no `🤖`, no model name in trailer or body. The human running the session is the author. No opt-out. Other `Co-Authored-By` trailers require explicit user request in the current conversation.
