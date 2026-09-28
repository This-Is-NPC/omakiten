package taskform

import (
	tea "github.com/charmbracelet/bubbletea"
)

// EventKind identifies an action that the parent TUI must handle.
type EventKind int

const (
	EventNone EventKind = iota
	EventSave
	EventCancel
	EventConfirmDiscard
	EventParentBlur
)

// Event carries form intent without coupling it to repositories.
type Event struct {
	Kind   EventKind
	Values Values
	Parent string
}

// ownsKey is the boundary between the two owners of this screen's keyboard.
//
// The form is the inner owner and screengrid the outer one, and the split is
// decided by whether the focused field TAKES CHARACTERS. Four of the five
// sections are a bubbles text input, and for those the answer is every
// spelling: `j`, `k`, `g` and `G` typed into Title are the letters j, k, g and
// G, not the arranger's motion vocabulary. The priority strip is the one
// section that takes no text, so there the spellings this table does not
// name — the motion vocabulary of screenlayout.StandardBindings, and nothing
// else — belong to the grid.
//
// It is the complement of [form.update] by construction: for a key this
// returns false for, update returns the form unchanged apart from disarming
// the discard confirmation, a nil command and an empty Event.
func (m form) ownsKey(key string) bool {
	if m.section != SectionPriority {
		return true
	}
	switch key {
	case "ctrl+s", "esc", "tab", "shift+tab", "left", "h", "right", "l":
		return true
	}
	return false
}

func (m form) update(msg tea.KeyMsg) (form, tea.Cmd, Event) {
	keyName := msg.String()
	if keyName != "esc" {
		m.discardArmed = false
	}
	switch keyName {
	case "ctrl+s":
		return m, nil, Event{Kind: EventSave, Values: m.values()}
	case "esc":
		if m.dirty() && !m.discardArmed {
			m.discardArmed = true
			return m, nil, Event{Kind: EventConfirmDiscard}
		}
		m.discardArmed = false
		return m, nil, Event{Kind: EventCancel}
	case "tab":
		return m.rotate(1)
	case "shift+tab":
		return m.rotate(-1)
	case "left", "h":
		if m.section == SectionPriority {
			return m.cyclePriority(-1), nil, Event{}
		}
	case "right", "l":
		if m.section == SectionPriority {
			return m.cyclePriority(1), nil, Event{}
		}
	}

	var cmd tea.Cmd
	switch m.section {
	case SectionTitle:
		m.title, cmd = m.title.Update(msg)
	case SectionDescription:
		m.description, cmd = m.description.Update(msg)
	case SectionTags:
		m.tags, cmd = m.tags.Update(msg)
	case SectionParent:
		m.parent, cmd = m.parent.Update(msg)
		if m.values().Parent == "" {
			m.parentError = ""
		}
	}
	return m, cmd, Event{}
}

func (m form) rotate(delta int) (form, tea.Cmd, Event) {
	previous := m.section
	sections := m.sections()
	index := 0
	for i, section := range sections {
		if section == m.section {
			index = i
			break
		}
	}
	index = (index + delta + len(sections)) % len(sections)
	m.section = sections[index]
	m.applyFocus()
	if previous == SectionParent {
		return m, nil, Event{Kind: EventParentBlur, Parent: m.values().Parent}
	}
	return m, nil, Event{}
}
