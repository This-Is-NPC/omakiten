package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/tui/components/choice"
)

type choiceDemo struct {
	props []prop
}

func newChoiceDemo(demoCtx) demo {
	return choiceDemo{props: []prop{
		choiceProp("mode", "which control mark each option wears", 0, "radio", "checkbox", "toggle"),
		numberProp("options", "how many rows to paint; 0 is empty", 3, 0, 12),
	}}
}

func (d choiceDemo) Resize(demoCtx) demo             { return d }
func (d choiceDemo) Update(tea.KeyMsg, demoCtx) demo { return d }

func (d choiceDemo) mode() choice.Mode {
	switch propLabel(d.props, "mode") {
	case "checkbox":
		return choice.Checkbox
	case "toggle":
		return choice.Toggle
	default:
		return choice.Radio
	}
}

func (d choiceDemo) options() []choice.Option {
	n := propNumber_(d.props, "options")
	labels := []string{"Active", "Candidate", "None"}
	details := []string{"current.yaml", "", "clear"}
	out := make([]choice.Option, n)
	mode := d.mode()
	for i := range out {
		label := labels[i%len(labels)]
		if i >= len(labels) {
			label = fmt.Sprintf("Option %d", i+1)
		}
		selected := i == 0
		if mode != choice.Radio {
			selected = i%2 == 0
		}
		detail := ""
		if i < len(details) {
			detail = details[i]
		}
		out[i] = choice.Option{Label: label, Detail: detail, Selected: selected}
	}
	return out
}

func (d choiceDemo) View(demoCtx) string {
	mode := d.mode()
	opts := d.options()
	if len(opts) == 0 {
		return ""
	}
	rows := make([]string, len(opts))
	for i, opt := range opts {
		row := choice.Row(mode, opt)
		if opt.Detail != "" {
			row += "  " + opt.Detail
		}
		rows[i] = row
	}
	return strings.Join(rows, "\n")
}

func (d choiceDemo) Status(demoCtx) string {
	opts := d.options()
	if len(opts) == 0 {
		return "empty — 0 options"
	}
	selected := 0
	for _, opt := range opts {
		if opt.Selected {
			selected++
		}
	}
	return fmt.Sprintf("%s · %d options · %d selected", propLabel(d.props, "mode"), len(opts), selected)
}

func (d choiceDemo) Props() []prop { return d.props }

func (d choiceDemo) SetProp(p prop, c demoCtx) demo {
	d.props = replaceProp(d.props, p)
	return d.Resize(c)
}

func (d choiceDemo) Help() []key.Binding { return statelessHelp() }
