// Package agent is the application-side service the MCP and CLI adapters
// call into. It owns the okt-* prompt resolution (persona + skills + laws +
// templates merged into a single action message), the DTO shapes returned
// to agents, and the helpers that summarise / truncate / redact context
// before it crosses the wire. Imports internal/app and internal/domain
// only — no adapters.
package agent
