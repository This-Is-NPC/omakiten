---
name: No Co-Authored-By trailers
severity: error
---
Never attribute a commit to an AI agent. No `Co-Authored-By: <model>` trailer, no `Generated with <tool>` footer, no model name in trailer or body. The human running the session is the author. No opt-out. Human `Co-Authored-By` trailers require explicit user request in this conversation.

Bad: trailer `Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>` appended automatically.
Good: subject + body only; if a human co-author is requested in this conversation, add their name and email.
