// Package tui is the bubbletea-based terminal UI. The root Model owns
// every screen (home, board, table, graph, task detail, comment detail,
// settings, stats, logs, help) and dispatches keystrokes to the focused
// surface. Render leaves live alongside in render_*.go; reusable
// pure-render helpers live under internal/tui/components/. Scroll math
// is canonicalised in components/scrollwindow so every list/grid panel
// shares one definition of "keep cursor on screen".
package tui
