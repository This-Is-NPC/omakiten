package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestViewChangeReturnsRefreshCmdAndDefersFold pins the async refresh
// contract: navigating away from a sub that ran refresh() used to call
// refreshCurrentView synchronously on the Update goroutine. After
// perf/tui-refresh-async the nav handler returns a tea.Cmd whose msg
// is folded on the next Update. The test reproduces a board-to-table
// nav and asserts (a) Update returns a non-nil cmd, (b) the cmd's msg
// is a refreshAfterViewChangeMsg with valid snap data, and (c) feeding
// the msg back through Update populates the entity slices.
func TestViewChangeReturnsRefreshCmdAndDefersFold(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	model.top = topTasks
	model.sub = subBoard

	// Drive a board → table nav via the sub-cycle key. Update returns
	// immediately with the heavy refresh captured inside the returned
	// tea.Cmd — the previous board view stays rendered until the worker's
	// msg lands.
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	got := updated.(Model)
	if got.sub != subTable {
		t.Fatalf("after / nav, sub = %v, want subTable (board→table)", got.sub)
	}
	if cmd == nil {
		t.Fatalf("Update(/) returned nil cmd, want async refresh cmd; got.sub=%v", got.sub)
	}
	msg := cmd()
	rmsg, ok := msg.(refreshAfterViewChangeMsg)
	if !ok {
		t.Fatalf("cmd produced %T, want refreshAfterViewChangeMsg", msg)
	}
	if rmsg.err != nil {
		t.Fatalf("refreshAfterViewChangeMsg.err = %v", rmsg.err)
	}
	if !rmsg.snapValid {
		t.Fatalf("refreshAfterViewChangeMsg.snapValid = false; worker did not populate snap")
	}

	// Fold the worker's msg back into the model.
	folded, _ := got.Update(rmsg)
	final := folded.(Model)
	if len(final.skills) == 0 {
		t.Fatalf("post-fold skills = empty, want the snapshot-sourced skill slice")
	}
}
