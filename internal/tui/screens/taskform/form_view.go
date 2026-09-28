package taskform

import (
	"strings"

	"omakiten/internal/tui/components/field"
	"omakiten/internal/tui/components/screenkit"
)

// view renders the four sections and a single active-section marker.
func (m form) view(width int, labels Labels, theme field.FormTheme) string {
	// Every field below subtracts its own frame from this width, so a
	// non-positive value reaches lipgloss as a negative Width on five inputs at
	// once and each of them silently paints unconstrained. Floor once, here,
	// rather than five times downstream.
	width = max(1, width)
	sections := []string{
		m.label(SectionTitle, labels.Title, theme),
		m.textField(m.title.View(), width, SectionTitle, theme),
		"",
		m.label(SectionDescription, labels.Description, theme),
		field.RenderArea(m.description, width, 8, m.section == SectionDescription, theme.Multiline),
	}
	if len(m.priorities) > 0 {
		sections = append(sections, "", m.label(SectionPriority, labels.Priority, theme), m.priorityField(width, theme))
	}
	sections = append(sections,
		"",
		m.label(SectionTags, labels.Tags, theme),
		m.textField(m.tags.View(), width, SectionTags, theme),
		"",
		m.label(SectionParent, labels.Parent, theme),
		m.textField(m.parent.View(), width, SectionParent, theme),
		m.parentErrorLine(theme),
	)
	return strings.Join(sections, "\n")
}

func (m form) priorityField(width int, theme field.FormTheme) string {
	labels := make([]string, len(m.priorities))
	active := -1
	for i, option := range m.priorities {
		labels[i] = screenkit.Sanitize(option.Label)
		if labels[i] == "" {
			labels[i] = screenkit.Sanitize(option.Value)
		}
		if option.Value == m.priority {
			active = i
		}
	}
	parts := make([]string, 0, 3)
	if active < 0 {
		parts = append(parts, theme.Hint.Render(strings.Join(labels, "  ")))
	} else {
		if active > 0 {
			parts = append(parts, theme.Hint.Render(strings.Join(labels[:active], "  ")))
		}
		parts = append(parts, theme.LabelActive.Render("["+labels[active]+"]"))
		if active+1 < len(labels) {
			parts = append(parts, theme.Hint.Render(strings.Join(labels[active+1:], "  ")))
		}
	}
	return m.textField(strings.Join(parts, "  "), width, SectionPriority, theme)
}

func (m form) label(section Section, label string, theme field.FormTheme) string {
	marker := "  // "
	style := theme.Label
	if m.section == section {
		marker = "> // "
		style = theme.LabelActive
	}
	return style.Render(marker + strings.ToUpper(screenkit.Sanitize(label)))
}

func (m form) textField(value string, width int, section Section, theme field.FormTheme) string {
	innerWidth := width - theme.Input.GetHorizontalFrameSize()
	if innerWidth < 8 {
		innerWidth = 8
	}
	style := theme.Input.Width(innerWidth)
	if m.section == section && theme.ActiveBorder != nil {
		style = style.BorderForeground(theme.ActiveBorder)
	}
	return style.Render(value)
}

func (m form) parentErrorLine(theme field.FormTheme) string {
	if m.parentError == "" {
		return ""
	}
	return theme.Hint.Render("  " + screenkit.Sanitize(m.parentError))
}
