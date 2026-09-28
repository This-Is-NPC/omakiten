package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/tui/components/choice"
	"omakiten/internal/tui/components/dropdown"
	"omakiten/internal/tui/components/screenkit"
)

type dropdownDemo struct {
	props []prop
}

func newDropdownDemo(demoCtx) demo {
	return dropdownDemo{props: []prop{
		choiceProp("join", "how Detail is attached after the label", 0, "spaced", "dash"),
		choiceProp("open", "expanded list, or the collapsed trigger", 0, "yes", "no"),
		choiceProp("mode", "control mark; mixed is the create-row case", 0, "radio", "checkbox", "mixed"),
		textProp("filter", "optional filter chrome; empty paints none", ""),
		numberProp("options", "how many rows; 0 is empty", 3, 0, 24),
		numberProp("cursor", "which option wears the marker", 0, 0, 23),
	}}
}

func (d dropdownDemo) Resize(demoCtx) demo             { return d }
func (d dropdownDemo) Update(tea.KeyMsg, demoCtx) demo { return d }

func (d dropdownDemo) join() dropdown.DetailJoin {
	if propLabel(d.props, "join") == "dash" {
		return dropdown.DetailDash
	}
	return dropdown.DetailSpaced
}

func (d dropdownDemo) options() []dropdown.Option {
	n := propNumber_(d.props, "options")
	modeName := propLabel(d.props, "mode")
	out := make([]dropdown.Option, n)
	for i := range out {
		label, detail, trailing, mode, selected := d.optionAt(i, n, modeName)
		out[i] = dropdown.Option{
			Label:    label,
			Detail:   detail,
			Trailing: trailing,
			Selected: selected,
			Mode:     mode,
		}
	}
	return out
}

func (d dropdownDemo) optionAt(i, n int, modeName string) (label, detail, trailing string, mode choice.Mode, selected bool) {
	labels := []string{"Active", "Candidate", "None"}
	details := []string{"current.yaml", "candidate.yaml", ""}
	label = labels[i%len(labels)]
	if i >= len(labels) {
		label = fmt.Sprintf("Option %d", i+1)
	}
	if i < len(details) {
		detail = details[i]
	}
	mode = choice.Radio
	selected = i == 0
	switch modeName {
	case "checkbox", "mixed":
		mode = choice.Checkbox
		selected = i%2 == 0
		switch i {
		case 0:
			label, detail = "Go", "Go engineering"
		case 1:
			label, detail = "SQLite", "Data persistence"
		}
		if modeName == "mixed" && i == n-1 && n > 0 {
			mode, selected = choice.Radio, false
			label, detail = "+ create", ""
		}
	default:
		if i == 1 {
			trailing = "CUSTOM"
		}
	}
	return label, detail, trailing, mode, selected
}

func (d dropdownDemo) spec() dropdown.Spec {
	return dropdown.Spec{
		Options: d.options(),
		Cursor:  propNumber_(d.props, "cursor"),
		Open:    propLabel(d.props, "open") == "yes",
		Filter:  propString(d.props, "filter"),
		Join:    d.join(),
	}
}

func (d dropdownDemo) View(c demoCtx) string {
	rows := dropdown.Rows(d.spec(), c.kit.CursorMarker(true), c.kit.CursorMarker(false), c.kit.Styles.Hint)
	if c.kit.Width > 0 {
		rows = screenkit.CapRows(rows, c.kit.Width)
	}
	if c.kit.Height > 0 && len(rows) > c.kit.Height {
		rows = rows[:c.kit.Height]
	}
	return strings.Join(rows, "\n")
}

func (d dropdownDemo) Status(demoCtx) string {
	spec := d.spec()
	if len(spec.Options) == 0 && spec.Filter == "" {
		return "empty — 0 options"
	}
	state := "open"
	if !spec.Open {
		state = "closed"
	}
	if spec.Filter != "" {
		return fmt.Sprintf("%s · %s · / %s · %d options", propLabel(d.props, "mode"), state, spec.Filter, len(spec.Options))
	}
	return fmt.Sprintf("%s · %s · %d options · cursor %d", propLabel(d.props, "mode"), state, len(spec.Options), spec.Cursor)
}

func (d dropdownDemo) Props() []prop { return d.props }

func (d dropdownDemo) SetProp(p prop, c demoCtx) demo {
	d.props = replaceProp(d.props, p)
	return d.Resize(c)
}

func (d dropdownDemo) Help() []key.Binding { return statelessHelp() }
