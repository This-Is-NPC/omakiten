// Package activity carries the per-call agent attribution context (model,
// session id, harness, tool) and the activity-tracker port adapter wires
// to log every MCP/CLI invocation to the database. Pure context plumbing —
// no I/O of its own; the storage adapter (internal/sqlite) implements the
// recording side.
package activity
