// Package paths owns Omakiten's runtime directory layout. Resolves the
// active config root ($OMAKITEN_HOME or per-OS XDG default), the data
// directory, the entity sub-folders (skills/, personas/, laws/, …), and
// the per-profile YAML location. Every consumer goes through these
// helpers so the layout stays consistent across the CLI, TUI, MCP, and
// installer scripts.
package paths
