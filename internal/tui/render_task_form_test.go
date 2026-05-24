package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// taskEditFormFixture builds a minimal Model in taskScreenEdit pointing
// at an in-memory task. Skips the canEditTask permission gate (no
// workflow loaded) by wiring the edit state directly rather than going
// through openTaskEdit. Repos stay empty so the parent-blur validation
// runs against an empty tasks slice — perfect for forcing "not found"
// without any DB plumbing.
func taskEditFormFixture(t *testing.T) Model {
	t.Helper()
	task := domain.Task{ID: 42, Title: "Original", BucketKey: "backlog", Priority: domain.Priority(2)}
	m := Model{
		styles: newStyles(config.Theme{}),
		width:  160,
		height: 40,
		tasks:  []domain.Task{task},
	}
	// Seed the form fields the way openTaskEdit would, minus the
	// permission gate and the tag/parent DB reads. The snapshot mirrors
	// the seeded inputs so taskEditFormDirty starts clean.
	m.taskScreen = taskScreenEdit
	m.taskID = task.ID
	m.taskTitleInput = newTaskTitleInput()
	m.taskTitleInput.SetValue(task.Title)
	m.taskTitleInput.SetCursor(len(task.Title))
	m.taskDescriptionInput = newTaskDescriptionInput()
	m.taskTagsInput = newTaskTagsInput()
	m.taskParentInput = newTaskParentInput()
	m.taskPriority = task.Priority
	m.taskEditInitial = taskEditSnapshot{
		active:      true,
		title:       task.Title,
		description: "",
		priority:    task.Priority,
		tagsCSV:     "",
		parent:      "",
	}
	m.taskField = taskFieldTitle
	m.applyTaskFieldFocus()
	return m
}

// dirtyTaskEditForm returns a fixture whose title has been mutated so
// taskEditFormDirty() reports true — the esc-pending arm path only
// engages on a dirty form.
func dirtyTaskEditForm(t *testing.T) Model {
	t.Helper()
	m := taskEditFormFixture(t)
	m.taskTitleInput.SetValue("Original edited")
	m.taskTitleInput.SetCursor(len("Original edited"))
	if !m.taskEditFormDirty() {
		t.Fatalf("dirtyTaskEditForm: expected dirty form, got clean")
	}
	return m
}

func sendKey(t *testing.T, m Model, key tea.KeyMsg) Model {
	t.Helper()
	updated, _ := m.updateTaskScreen(key)
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("updateTaskScreen returned %T, want Model", updated)
	}
	return got
}

// TestTaskEscArmOrConfirm_TwoEscsWhileDirtyConfirms pins the arm-then-
// confirm contract: first esc on a dirty edit arms the discard prompt
// (form stays open, status hint set); second esc closes the form back
// to the task view. Pre-fix this lived as a scatter of 7+ flag
// mutations; post-fix it runs through taskEscArmOrConfirm.
func TestTaskEscArmOrConfirm_TwoEscsWhileDirtyConfirms(t *testing.T) {
	m := dirtyTaskEditForm(t)

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.taskScreen != taskScreenEdit {
		t.Fatalf("after first esc taskScreen = %v, want taskScreenEdit (armed)", m.taskScreen)
	}
	if !m.taskEscPendingDiscard {
		t.Fatalf("after first esc taskEscPendingDiscard = false, want true (armed)")
	}

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.taskScreen == taskScreenEdit {
		t.Fatalf("after second esc taskScreen = taskScreenEdit, want closed-or-view (confirmed)")
	}
	if m.taskEscPendingDiscard {
		t.Fatalf("after second esc taskEscPendingDiscard = true, want false (cleared on close)")
	}
}

// TestTaskEscDisarm_OtherKeyResetsArm guards the disarm catchall: an
// intervening non-esc keystroke must clear the arm so the next esc
// counts as a fresh "arm", not the second half of a confirm pair. Pre-
// fix this worked accidentally because nearly every handler manually
// reset the flag; post-fix it's funnelled through taskEscDisarm.
func TestTaskEscDisarm_OtherKeyResetsArm(t *testing.T) {
	m := dirtyTaskEditForm(t)

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if !m.taskEscPendingDiscard {
		t.Fatalf("after first esc taskEscPendingDiscard = false, want true (armed)")
	}

	// Any non-esc keypress disarms. A rune keystroke ends up routed to
	// the focused input but the catchall must fire first.
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if m.taskEscPendingDiscard {
		t.Fatalf("after non-esc keystroke taskEscPendingDiscard = true, want false (disarmed)")
	}

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.taskScreen != taskScreenEdit {
		t.Fatalf("after re-armed esc taskScreen = %v, want taskScreenEdit (only armed, not confirmed)", m.taskScreen)
	}
	if !m.taskEscPendingDiscard {
		t.Fatalf("after re-armed esc taskEscPendingDiscard = false, want true (re-armed)")
	}
}

// TestTaskParentLookupError_PersistsAcrossUnrelatedKeystrokes pins Bug
// 2: the parent-input keystroke handler used to clear
// taskParentLookupError on every key, so the user typing a correction
// saw the hint flash and vanish mid-edit. Post-fix the error survives
// unrelated keystrokes and only clears on (a) input emptied to "" via
// keystroke, (b) successful re-blur, or (c) form close.
//
// The fixture seeds taskParentLookupError directly rather than driving
// validateParentInputOnBlur — that path needs a live Tasks repo and a
// project context which are out of scope for this unit. We're testing
// the keystroke handler's clear policy, not the lookup itself.
func TestTaskParentLookupError_PersistsAcrossUnrelatedKeystrokes(t *testing.T) {
	m := taskEditFormFixture(t)

	// Move focus to Parent and seed both the invalid input and the
	// post-blur error state directly (skip the validate path which
	// needs repos).
	m.taskField = taskFieldParent
	m.applyTaskFieldFocus()
	m.taskParentInput.SetValue("999")
	m.taskParentInput.SetCursor(len("999"))
	priorError := m.t("tui.taskedit.parent_lookup_not_found")
	m.taskParentLookupError = priorError

	// Typing on the parent field must not clear the error mid-edit —
	// the user needs to read it long enough to act on it. Backspace
	// (delete one digit) plus a fresh keystroke (type a new digit)
	// covers both removal and insertion paths through the input.
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	if m.taskParentLookupError != priorError {
		t.Fatalf("after backspace on parent field taskParentLookupError = %q, want %q (must persist across unrelated keystrokes)", m.taskParentLookupError, priorError)
	}
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'7'}})
	if m.taskParentLookupError != priorError {
		t.Fatalf("after typing on parent field taskParentLookupError = %q, want %q (must persist across unrelated keystrokes)", m.taskParentLookupError, priorError)
	}

	// Clearing the parent input to empty IS a legitimate clear site —
	// the next render shouldn't surface a stale hint against an empty
	// field. Backspacing the remaining "97" (after the edits above)
	// down to "" should trip the clear.
	for m.taskParentInput.Value() != "" {
		m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	}
	if m.taskParentLookupError != "" {
		t.Fatalf("after emptying parent input taskParentLookupError = %q, want \"\" (cleared on empty)", m.taskParentLookupError)
	}
}
