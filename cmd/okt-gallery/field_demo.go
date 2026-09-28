package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/field"
)

const fieldAreaBorderRows = 2

// fieldDemo is the two surfaces package field owns, selected by shape:
// line (one prompt) and area (a textarea in bordered chrome).
type fieldDemo struct {
	input textarea.Model
	props []prop
}

func newFieldDemo(c demoCtx) demo {
	input := textarea.New()
	input.Placeholder = "describe the task…"
	input.SetValue(sampleProse)
	input.Focus()
	d := fieldDemo{input: input, props: []prop{
		choiceProp("shape", "line · area — one prompt, or a textarea. Not one tea.Model", 0, "line", "area"),
		textProp("label", "the prompt before the colon (line)", "target bucket"),
		textProp("value", "what has been typed so far (line)", "dev"),
		numberProp("floor", "columns the line input keeps even when the label will not fit", 12, 1, 60),
		autoProp("available", "the room the line is given — auto follows the frame", 200),
		autoProp("width", "the OUTER cell width RenderArea takes", 300),
		autoProp("height", "the textarea's visible row count", 200),
		choiceProp("focused", "area: swaps the border to BorderActive", 0, "yes", "no"),
		textProp("content", "the value the textarea is seeded with", sampleProse),
	}}
	return d.Resize(c)
}

func (d fieldDemo) shape() string { return propLabel(d.props, "shape") }

func (d fieldDemo) lineField() textinput.Model {
	input := textinput.New()
	input.Prompt = ""
	input.SetValue(propString(d.props, "value"))
	return input
}

func (d fieldDemo) Resize(c demoCtx) demo {
	if d.shape() != "area" {
		return d
	}
	theme := fieldTheme(c)
	field.Resize(&d.input, d.areaWidth(c, theme), d.areaHeight(c), theme)
	return d
}

func (d fieldDemo) Update(msg tea.KeyMsg, _ demoCtx) demo {
	if d.shape() != "area" {
		return d
	}
	// While the area has focus every key is content, so none can be reserved.
	d.input, _ = d.input.Update(msg)
	return d
}

func (d fieldDemo) View(c demoCtx) string {
	if d.shape() == "area" {
		theme := fieldTheme(c)
		return field.RenderArea(d.input, d.areaWidth(c, theme), d.areaHeight(c),
			propOption(d.props, "focused") == 0, theme)
	}
	label, available, _ := d.lineGeometry(c)
	label, width := field.Fit(c.kit.Styles, label, available, propNumber_(d.props, "floor"))
	return field.RenderLine(c.kit.Styles, label, d.lineField(), width)
}

func (d fieldDemo) lineGeometry(c demoCtx) (label string, available, width int) {
	label = propString(d.props, "label")
	available = propAuto(d.props, "available", c.kit.Width)
	width = field.Width(c.kit.Styles, available, lipgloss.Width(label+": "), propNumber_(d.props, "floor"))
	return label, available, width
}

func (d fieldDemo) areaWidth(c demoCtx, theme field.Theme) int {
	return maxInt(propAuto(d.props, "width", field.WidthFor(c.kit.Width, theme)), 1)
}

func (d fieldDemo) areaHeight(c demoCtx) int {
	return maxInt(propAuto(d.props, "height", c.kit.Height-fieldAreaBorderRows), 1)
}

func (d fieldDemo) Status(c demoCtx) string {
	if d.shape() == "area" {
		return fmt.Sprintf("%d lines typed · while the frame has focus every key is content", d.input.LineCount())
	}
	label, available, _ := d.lineGeometry(c)
	label, width := field.Fit(c.kit.Styles, label, available, propNumber_(d.props, "floor"))
	drawn := lipgloss.Width(strings.Split(d.View(c), "\n")[0])
	overflow := ""
	if drawn > available {
		overflow = fmt.Sprintf(" · OVERFLOWS by %d — the floor won, which beats an input too narrow to type in", drawn-available)
	}
	return fmt.Sprintf("label %d + typing room %d in %d available → row is %d columns%s",
		lipgloss.Width(label+": "), width, available, drawn, overflow)
}

func (d fieldDemo) Props() []prop { return d.props }

func (d fieldDemo) SetProp(p prop, c demoCtx) demo {
	d.props = replaceProp(d.props, p)
	if p.name == "content" {
		d.input.SetValue(p.text)
	}
	return d.Resize(c)
}

func (d fieldDemo) Help() []key.Binding {
	if d.shape() == "area" {
		return []key.Binding{binding("type", "it is a real textarea")}
	}
	return statelessHelp()
}

func fieldTheme(c demoCtx) field.Theme {
	return field.Theme{
		Border:       c.kit.Styles.FormMultiline,
		BorderActive: c.kit.Styles.HintAccent.GetForeground(),
		Cursor:       c.kit.Styles.Cursor,
	}
}
