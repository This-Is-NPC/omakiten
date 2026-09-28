package taskform

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/field"
)

func TestModelFocusRotationAndParentBlur(t *testing.T) {
	t.Parallel()

	m := newForm(Values{Title: "Task", Description: "Body", TagsCSV: "one, two", Parent: "7"}, 80, Theme{})
	if got := m.activeSection(); got != SectionTitle {
		t.Fatalf("initial section = %v, want title", got)
	}

	for _, want := range []Section{SectionDescription, SectionTags, SectionParent} {
		var event Event
		m, _, event = m.update(tea.KeyMsg{Type: tea.KeyTab})
		if got := m.activeSection(); got != want {
			t.Fatalf("section after tab = %v, want %v", got, want)
		}
		if event.Kind != EventNone {
			t.Fatalf("event while entering %v = %v, want none", want, event.Kind)
		}
	}

	m, _, event := m.update(tea.KeyMsg{Type: tea.KeyTab})
	if got := m.activeSection(); got != SectionTitle {
		t.Fatalf("wrapped section = %v, want title", got)
	}
	if event.Kind != EventParentBlur || event.Parent != "7" {
		t.Fatalf("parent blur event = %#v, want parent 7", event)
	}

	m, _, _ = m.update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if got := m.activeSection(); got != SectionParent {
		t.Fatalf("reverse wrapped section = %v, want parent", got)
	}
}

func TestModelSaveAndDirtyCancellation(t *testing.T) {
	t.Parallel()

	m := newForm(Values{Title: "Task"}, 80, Theme{})
	m, _, _ = m.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'!'}})
	if !m.dirty() {
		t.Fatal("Dirty() = false after title edit, want true")
	}

	m, _, event := m.update(tea.KeyMsg{Type: tea.KeyEsc})
	if event.Kind != EventConfirmDiscard {
		t.Fatalf("first dirty esc event = %v, want confirm discard", event.Kind)
	}
	m, _, event = m.update(tea.KeyMsg{Type: tea.KeyEsc})
	if event.Kind != EventCancel {
		t.Fatalf("second dirty esc event = %v, want cancel", event.Kind)
	}

	_, _, event = m.update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if event.Kind != EventSave {
		t.Fatalf("ctrl+s event = %v, want save", event.Kind)
	}
	if got := event.Values.Title; got != "Task!" {
		t.Fatalf("saved title = %q, want Task!", got)
	}
}

func TestModelPrioritySectionCyclesAndParticipatesInDirtyState(t *testing.T) {
	t.Parallel()

	m := newForm(Values{Title: "Task", Priority: "2"}, 60, Theme{}).withPriorities([]PriorityOption{
		{Value: "1", Label: "low"}, {Value: "2", Label: "normal"}, {Value: "3", Label: "high"},
	})
	if !m.isActive() || m.parentError != "" || m.confirmingDiscard() {
		t.Fatal("new model lifecycle state mismatch")
	}
	for range 2 {
		m, _, _ = m.update(tea.KeyMsg{Type: tea.KeyTab})
	}
	if m.activeSection() != SectionPriority {
		t.Fatalf("active section = %v, want priority", m.activeSection())
	}
	m, _, _ = m.update(tea.KeyMsg{Type: tea.KeyRight})
	if m.values().Priority != "3" || !m.dirty() {
		t.Fatalf("priority=%q dirty=%v, want 3/true", m.values().Priority, m.dirty())
	}
	view := m.view(60, Labels{Title: "Title", Description: "Description", Priority: "Priority", Tags: "Tags", Parent: "Parent"}, Theme{})
	for _, want := range []string{"PRIORITY", "low", "normal", "[high]"} {
		if !strings.Contains(view, want) {
			t.Fatalf("priority view missing %q\n%s", want, view)
		}
	}
	m, _, _ = m.update(tea.KeyMsg{Type: tea.KeyLeft})
	m, _, _ = m.update(tea.KeyMsg{Type: tea.KeyLeft})
	if m.values().Priority != "1" {
		t.Fatalf("clamped priority = %q, want 1", m.values().Priority)
	}
}

func TestModelCleanCancelAndEmptyParentMeansRoot(t *testing.T) {
	t.Parallel()

	m := newForm(Values{Title: "Task"}, 80, Theme{})
	m, _, event := m.update(tea.KeyMsg{Type: tea.KeyEsc})
	if event.Kind != EventCancel {
		t.Fatalf("clean esc event = %v, want cancel", event.Kind)
	}

	for range 3 {
		m, _, _ = m.update(tea.KeyMsg{Type: tea.KeyTab})
	}
	_, _, event = m.update(tea.KeyMsg{Type: tea.KeyTab})
	if event.Kind != EventParentBlur || event.Parent != "" {
		t.Fatalf("empty parent blur = %#v, want root", event)
	}
}

func TestDirtyIgnoresTagOrderAndWhitespace(t *testing.T) {
	t.Parallel()

	m := newForm(Values{Title: "Task", TagsCSV: "alpha, beta"}, 80, Theme{})
	for range 2 {
		m, _, _ = m.update(tea.KeyMsg{Type: tea.KeyTab})
	}
	for range len("alpha, beta") {
		m, _, _ = m.update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	for _, r := range " beta , alpha " {
		m, _, _ = m.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if m.dirty() {
		t.Fatal("Dirty() = true for equivalent reordered tags, want false")
	}
}

func TestViewMarksOnlyActiveSectionAndShowsParentError(t *testing.T) {
	t.Parallel()

	theme := Theme{
		Label:        lipgloss.NewStyle(),
		LabelActive:  lipgloss.NewStyle(),
		Input:        lipgloss.NewStyle().Border(lipgloss.NormalBorder()),
		ActiveBorder: lipgloss.Color("2"),
		Hint:         lipgloss.NewStyle(),
	}
	m := newForm(Values{Title: "Task"}, 60, theme)
	m = m.withParentError("parent not found")
	got := m.view(60, Labels{Title: "Title", Description: "Description", Tags: "Tags", Parent: "Parent"}, theme)

	if count := strings.Count(got, "> // "); count != 1 {
		t.Fatalf("active markers = %d, want 1\n%s", count, got)
	}
	for _, text := range []string{"TITLE", "DESCRIPTION", "TAGS", "PARENT", "parent not found"} {
		if !strings.Contains(got, text) {
			t.Fatalf("View() missing %q\n%s", text, got)
		}
	}
}

func TestViewUsesAvailableWidthForSingleLineInputs(t *testing.T) {
	t.Parallel()

	theme := Theme{Input: lipgloss.NewStyle().Border(lipgloss.NormalBorder())}
	title := "A title longer than twenty columns"
	m := newForm(Values{Title: title}, 60, theme)
	got := m.view(60, Labels{Title: "Title", Description: "Description", Tags: "Tags", Parent: "Parent"}, theme)

	if !strings.Contains(got, title) {
		t.Fatalf("View() clipped title despite available width\n%s", got)
	}
}

func TestResizeRecalibratesDescriptionInput(t *testing.T) {
	t.Parallel()

	theme := Theme{Multiline: field.Theme{Border: lipgloss.NewStyle().Padding(0, 2)}}
	m := newForm(Values{Description: "Body"}, 80, theme)
	m = m.resize(44, theme)

	if got, want := m.description.Width(), 40; got != want {
		t.Fatalf("description width after Resize() = %d, want %d", got, want)
	}
	if got := m.description.Height(); got != 8 {
		t.Fatalf("description height after Resize() = %d, want 8", got)
	}
	for name, got := range map[string]int{
		"title":  m.title.Width,
		"tags":   m.tags.Width,
		"parent": m.parent.Width,
	} {
		if got != 44 {
			t.Fatalf("%s width after Resize() = %d, want 44", name, got)
		}
	}
}

func TestResizeAppliesCursorStyleToSingleLineInputs(t *testing.T) {
	t.Parallel()

	cursor := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	m := newForm(Values{Title: "Task"}, 60, Theme{Cursor: cursor})
	if got := m.title.Cursor.Style.GetForeground(); got != cursor.GetForeground() {
		t.Fatalf("title cursor foreground = %v, want %v", got, cursor.GetForeground())
	}
	if got := m.tags.Cursor.Style.GetForeground(); got != cursor.GetForeground() {
		t.Fatalf("tags cursor foreground = %v, want %v", got, cursor.GetForeground())
	}
	if got := m.parent.Cursor.Style.GetForeground(); got != cursor.GetForeground() {
		t.Fatalf("parent cursor foreground = %v, want %v", got, cursor.GetForeground())
	}
}
