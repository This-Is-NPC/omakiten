package main

import (
	"fmt"
	"strconv"
	"strings"
)

// prop is one editable input of a component's own API — a viewport budget, a
// column width, the text of a header. Not a knob the gallery invented: every
// prop here corresponds to an argument or a field the component really takes,
// which is what makes editing one at runtime an experiment rather than a demo.
type prop struct {
	name string
	kind propKind
	// note is the one-line reminder of what the component does with it.
	note string

	// number
	number   int
	min, max int
	// zeroLabel names the meaning of 0 for geometry props that follow the frame
	// unless overridden. Empty when 0 is just zero.
	zeroLabel string

	// choice
	options  []string
	selected int

	// text
	text string
}

type propKind int

const (
	propNumber propKind = iota
	propChoice
	propText
)

func numberProp(name, note string, value, min, max int) prop {
	return prop{name: name, kind: propNumber, note: note, number: value, min: min, max: max}
}

// autoProp is a geometry number that follows the frame while it is zero. It is
// the honest default for anything a screen would normally derive rather than
// hardcode — you can still pin it to watch what a wrong number does.
func autoProp(name, note string, max int) prop {
	p := numberProp(name, note, 0, 0, max)
	p.zeroLabel = "auto"
	return p
}

func choiceProp(name, note string, selected int, options ...string) prop {
	return prop{name: name, kind: propChoice, note: note, options: options, selected: selected}
}

func textProp(name, note, value string) prop {
	return prop{name: name, kind: propText, note: note, text: value}
}

// display is what the Properties panel shows for a number or text prop.
func (p prop) display() string {
	switch p.kind {
	case propText:
		return p.text
	case propChoice:
		if p.selected >= 0 && p.selected < len(p.options) {
			return p.options[p.selected]
		}
		return ""
	default:
		if p.number == 0 && p.zeroLabel != "" {
			return p.zeroLabel
		}
		return strconv.Itoa(p.number)
	}
}

// withRaw parses a value written as text — what a text box produces, and what a
// scenario declares — into whatever this prop actually holds. An unparseable
// number or an unknown option leaves the prop alone, so a half-typed value never
// snaps the component to something it was not asked for.
func (p prop) withRaw(raw string) prop {
	raw = strings.TrimSpace(raw)
	switch p.kind {
	case propText:
		p.text = raw
	case propChoice:
		for i, option := range p.options {
			if strings.EqualFold(option, raw) {
				p.selected = i
			}
		}
	default:
		if raw == "" || strings.EqualFold(raw, p.zeroLabel) {
			p.number = 0
			return p
		}
		if v, err := strconv.Atoi(raw); err == nil {
			p.number = clamp(v, p.min, p.max)
		}
	}
	return p
}

func (p prop) cycle(delta int) prop {
	if p.kind != propChoice || len(p.options) == 0 {
		return p
	}
	total := len(p.options)
	p.selected = ((p.selected+delta)%total + total) % total
	return p
}

// resolve is the geometry read every demo does: the prop's value, or the frame's
// when it is on auto.
func (p prop) resolve(frame int) int {
	if p.number == 0 && p.zeroLabel != "" {
		return frame
	}
	return p.number
}

// ---- prop sets -------------------------------------------------------------

func findProp(props []prop, name string) (prop, bool) {
	for _, p := range props {
		if p.name == name {
			return p, true
		}
	}
	return prop{}, false
}

func propNumber_(props []prop, name string) int {
	p, _ := findProp(props, name)
	return p.number
}

func propAuto(props []prop, name string, frame int) int {
	p, ok := findProp(props, name)
	if !ok {
		return frame
	}
	return p.resolve(frame)
}

func propOption(props []prop, name string) int {
	p, _ := findProp(props, name)
	return p.selected
}

// propLabel is the selected option's LABEL rather than its index. Use it when
// the option text is itself the value the component takes — a colour token, a
// family name — so the switch reads as the thing instead of as a position.
func propLabel(props []prop, name string) string {
	p, _ := findProp(props, name)
	if p.selected < 0 || p.selected >= len(p.options) {
		return ""
	}
	return p.options[p.selected]
}

func propString(props []prop, name string) string {
	p, _ := findProp(props, name)
	return p.text
}

// replaceProp returns a copy of the set with one prop swapped. Copying rather
// than mutating keeps the demos value types, the same as every component they
// exhibit.
func replaceProp(props []prop, updated prop) []prop {
	out := make([]prop, len(props))
	copy(out, props)
	for i := range out {
		if out[i].name == updated.name {
			out[i] = updated
		}
	}
	return out
}

// seed applies a scenario's declared values to a prop set. Unknown names are
// ignored so a scenario can name a prop that only some components have.
func seed(props []prop, values map[string]string) []prop {
	out := make([]prop, len(props))
	copy(out, props)
	for i := range out {
		if raw, ok := values[out[i].name]; ok {
			out[i] = out[i].withRaw(raw)
		}
	}
	return out
}

// ---- scenarios -------------------------------------------------------------

// scenario is a named preset: one pick that puts the frame AND the component
// into a whole situation at once.
//
// It SEEDS and does not lock. Every value it writes stays editable underneath —
// a scenario is a shortcut to a starting point, not a mode.
type scenario struct {
	name string
	note string

	// Frame geometry. Zero means "leave whatever is there", so a scenario about
	// the component alone does not disturb the size you were looking at.
	width, height, padding int
	// Alignment by name ("top", "center", …). Empty means leave.
	vertical, horizontal string

	// props are component property values, written the way a text box would
	// write them: numbers as digits, choices as the option's label.
	props map[string]string
}

func (s scenario) label() string {
	if s.note == "" {
		return s.name
	}
	return s.name
}

// summary is the one-liner shown under the scenario list.
func (s scenario) summary() string {
	parts := make([]string, 0, 4)
	if s.width > 0 || s.height > 0 {
		parts = append(parts, fmt.Sprintf("%s×%s", orAuto(s.width), orAuto(s.height)))
	}
	if s.padding > 0 {
		parts = append(parts, fmt.Sprintf("pad %d", s.padding))
	}
	for name, value := range s.props {
		parts = append(parts, name+" "+value)
	}
	if s.note != "" {
		return s.note
	}
	return strings.Join(parts, " · ")
}

func orAuto(v int) string {
	if v <= 0 {
		return "auto"
	}
	return strconv.Itoa(v)
}
