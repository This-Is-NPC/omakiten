// Package graph holds the dependency-graph helpers used by both the TUI
// graph view and the CLI/MCP graph endpoints. Pure logic: takes a slice
// of tasks + dependencies, returns an adjacency list / topological
// ordering / DAG line listing. No I/O.
package graph
