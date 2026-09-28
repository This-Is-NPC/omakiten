// Package screenhost defines the dependency-neutral contract between the TUI
// root and independently extractable screens. It owns stable screen identity,
// immutable frame snapshots, semantic outcomes, and descriptor metadata; it
// never imports the root internal/tui package.
package screenhost
