package taskform

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"

	"omakiten/internal/taskvalidation"
	"omakiten/internal/tui/components/field"
	"omakiten/internal/tui/components/screenkit"
)

// Section identifies the field that currently owns keyboard focus.
type Section int

const (
	SectionTitle Section = iota
	SectionDescription
	SectionPriority
	SectionTags
	SectionParent
)

var sectionOrder = [...]Section{SectionTitle, SectionDescription, SectionPriority, SectionTags, SectionParent}

// Values is the persistence-neutral projection of an edit form.
type Values struct {
	Title       string
	Description string
	Priority    string
	TagsCSV     string
	Parent      string
}

// PriorityOption is one configured priority rendered by the form.
type PriorityOption struct {
	Value string
	Label string
}

// Labels contains the user-facing section names supplied by the TUI adapter.
type Labels struct {
	Title       string
	Description string
	Priority    string
	Tags        string
	Parent      string
}

// Theme is the form chrome this screen paints with, owned by components/field
// and named here only so the host's Deps keep one spelling.
//
// It used to be six bare style fields declared right here, which made the
// screen the author of a style vocabulary instead of a consumer of one — and
// left its fixture free to invent a palette out of hex literals that no theme
// could reach. Build one with [field.Form] from the Styles the Kit carries;
// nothing constructs the fields by hand any more.
type Theme = field.FormTheme

// form owns all mutable widget state for one task create/edit session.
type form struct {
	title        textinput.Model
	description  textarea.Model
	tags         textinput.Model
	parent       textinput.Model
	priorities   []PriorityOption
	priority     string
	initial      Values
	section      Section
	active       bool
	discardArmed bool
	parentError  string
}

func newForm(values Values, width int, theme Theme) form {
	m := form{
		title:       newTextInput(values.Title),
		description: newDescriptionInput(values.Description),
		tags:        newTextInput(values.TagsCSV),
		parent:      newTextInput(values.Parent),
		priority:    values.Priority,
		initial:     values,
		section:     SectionTitle,
		active:      true,
	}
	m = m.resize(width, theme)
	m.applyFocus()
	return m
}

// withPriorities configures the priority section. An empty option set omits the
// section, preserving the four-field shape used before priorities became part
// of the shared create/edit screen.
func (m form) withPriorities(options []PriorityOption) form {
	m.priorities = append([]PriorityOption(nil), options...)
	if m.priority == "" && len(m.priorities) > 0 {
		m.priority = m.priorities[0].Value
	}
	m.initial.Priority = m.priority
	return m
}

func newTextInput(value string) textinput.Model {
	value = screenkit.Sanitize(value)
	input := textinput.New()
	input.Prompt = ""
	input.CharLimit = 0
	input.SetValue(value)
	input.SetCursor(len(value))
	return input
}

func newDescriptionInput(value string) textarea.Model {
	value = screenkit.SanitizeMultiline(value)
	input := textarea.New()
	input.Prompt = ""
	input.ShowLineNumbers = false
	input.CharLimit = 0
	input.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("enter", "shift+enter", "alt+enter", "ctrl+j", "ctrl+m"),
	)
	input.SetValue(value)
	input.CursorEnd()
	return input
}

func (m form) values() Values {
	return Values{
		Title:       strings.TrimSpace(m.title.Value()),
		Description: strings.TrimSpace(m.description.Value()),
		Priority:    m.priority,
		TagsCSV:     strings.TrimSpace(m.tags.Value()),
		Parent:      strings.TrimSpace(m.parent.Value()),
	}
}

func (m form) activeSection() Section { return m.section }

func (m form) isActive() bool { return m.active }

func (m form) dirty() bool {
	current := m.values()
	initial := Values{
		Title:       strings.TrimSpace(m.initial.Title),
		Description: strings.TrimSpace(m.initial.Description),
		Priority:    m.initial.Priority,
		TagsCSV:     strings.TrimSpace(m.initial.TagsCSV),
		Parent:      strings.TrimSpace(m.initial.Parent),
	}
	current.TagsCSV = taskvalidation.NormalizeTags(current.TagsCSV)
	initial.TagsCSV = taskvalidation.NormalizeTags(initial.TagsCSV)
	return current != initial
}

func (m form) sections() []Section {
	if len(m.priorities) > 0 {
		return sectionOrder[:]
	}
	return []Section{SectionTitle, SectionDescription, SectionTags, SectionParent}
}

func (m form) cyclePriority(delta int) form {
	if len(m.priorities) == 0 {
		return m
	}
	index := 0
	for i, option := range m.priorities {
		if option.Value == m.priority {
			index = i
			break
		}
	}
	index += delta
	if index < 0 {
		index = 0
	}
	if index >= len(m.priorities) {
		index = len(m.priorities) - 1
	}
	m.priority = m.priorities[index].Value
	return m
}

func (m form) withParentError(message string) form {
	m.parentError = message
	return m
}

func (m form) confirmingDiscard() bool { return m.discardArmed }

func (m form) resize(width int, theme Theme) form {
	inputWidth := width - theme.Input.GetHorizontalFrameSize()
	if inputWidth < 8 {
		inputWidth = 8
	}
	m.title.Width = inputWidth
	m.tags.Width = inputWidth
	m.parent.Width = inputWidth
	m.title.Cursor.Style = theme.Cursor
	m.tags.Cursor.Style = theme.Cursor
	m.parent.Cursor.Style = theme.Cursor
	field.Resize(&m.description, width, 8, theme.Multiline)
	return m
}

func (m *form) applyFocus() {
	if m.section == SectionTitle {
		m.title.Focus()
	} else {
		m.title.Blur()
	}
	if m.section == SectionDescription {
		m.description.Focus()
	} else {
		m.description.Blur()
	}
	if m.section == SectionTags {
		m.tags.Focus()
	} else {
		m.tags.Blur()
	}
	if m.section == SectionParent {
		m.parent.Focus()
	} else {
		m.parent.Blur()
	}
}
