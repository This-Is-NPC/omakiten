// Package plan holds the dependency-graph projection a plan is read through:
// the blocker index scoped to and across waves, the intra-wave rail tree, the
// critical path, the left-margin lane allocation for cross-wave edges, and the
// completion percentage. Pure logic over internal/domain plan types — no I/O,
// no styles, no terminal — so the CLI, the agent surface and the TUI can all
// project a plan the same way.
package plan
