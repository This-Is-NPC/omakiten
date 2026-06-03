// Package token counts approximate token usage for MCP responses and
// agent-facing output. Two implementations: a tiktoken-backed BPE counter
// (network bootstrap on first use; falls back to ApproxCounter on
// failure) and the stdlib-only ApproxCounter (chars/4 heuristic). Same
// Counter port so renderers swap between them based on config.
package token
