package tui

import (
	"sync/atomic"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/tui/screenhost"
)

// TestViewChangeRefreshRegistryShrinksAfterFold pins the W2 #3
// finding: applyRefreshAfterViewChange must deregister the cmd
// pointer from viewChangeRefreshRegistry so a long-running TUI
// session does not leak one entry per nav.
func TestViewChangeRefreshRegistryShrinksAfterFold(t *testing.T) {
	clearViewChangeRefreshRegistry()
	model := buildRefreshHotPathModel(t)
	model.navigation = screenhost.TasksBoard

	// Drive enough nav cycles to exercise the registry on every refresh.
	const navCount = 8
	for i := 0; i < navCount; i++ {
		updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
		got := updated.(Model)
		if cmd == nil {
			t.Fatalf("Update(/) nav %d returned nil cmd", i)
		}
		msg := cmd()
		folded, _ := got.Update(msg)
		model = folded.(Model)
	}

	after := countViewChangeRefreshRegistry()
	if after > 1 {
		t.Fatalf("viewChangeRefreshRegistry leaked %d entries after %d nav cycles (want <= 1)", after, navCount)
	}
}

// clearViewChangeRefreshRegistry resets the package-level sync.Map so
// tests run in isolation regardless of prior test ordering. Production
// code never clears the registry — this is test-only.
func clearViewChangeRefreshRegistry() {
	viewChangeRefreshRegistry.Range(func(k, _ any) bool {
		viewChangeRefreshRegistry.Delete(k)
		return true
	})
}

// countViewChangeRefreshRegistry returns the live entry count.
func countViewChangeRefreshRegistry() int {
	var n atomic.Int64
	viewChangeRefreshRegistry.Range(func(_, _ any) bool {
		n.Add(1)
		return true
	})
	return int(n.Load())
}
