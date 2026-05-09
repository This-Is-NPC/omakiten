// Package token counts approximate token usage for context dumps and
// MCP responses. Two implementations: a tiktoken-backed BPE counter
// (network bootstrap on first use; falls back to ApproxCounter on
// failure) and the stdlib-only ApproxCounter (chars/4 heuristic). Same
// Counter port so renderers swap between them based on config.
package token
