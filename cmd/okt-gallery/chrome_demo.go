package main

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/header"
	"omakiten/internal/tui/components/tokenstrip"
)

// ---- header -----------------------------------------------------------------

type headerDemo struct {
	props []prop
}

func newHeaderDemo(demoCtx) demo {
	return headerDemo{props: []prop{
		choiceProp("state", "which of the four header states to paint", 0,
			"full", "compact", "overlay", "home"),
		textProp("segment", "breadcrumb project slug (ignored on home)", "demo"),
		choiceProp("subs", "whether the sub strip is present", 0, "yes", "no"),
	}}
}

func (d headerDemo) Resize(demoCtx) demo             { return d }
func (d headerDemo) Update(tea.KeyMsg, demoCtx) demo { return d }

func (d headerDemo) opts(c demoCtx) (header.Styles, header.Options) {
	styles := header.Styles{
		Title:     c.kit.Styles.Info.Bold(true),
		Nav:       c.kit.Styles.Nav,
		ActiveNav: c.kit.Styles.ActiveNav,
		Hint:      c.kit.Styles.Hint,
	}
	opts := header.Options{
		Width:          c.kit.Width,
		Brand:          "omakiten",
		Segment:        propString(d.props, "segment"),
		SegmentHint:    "local checkpoint",
		HomeLabel:      "00 // HOME",
		CompactHint:    "  tab/1-3 switch zones · ,// switch sub · 0 home · ctrl+o back",
		HomeReturnHint: "  ctrl+h returns here from any view",
		Tops: []header.Item{
			{Label: "01 // TASKS", Active: true},
			{Label: "02 // STATS"},
			{Label: "03 // STUDIO"},
			{Label: "04 // SETTINGS"},
		},
	}
	if propLabel(d.props, "subs") == "yes" {
		opts.Subs = []header.Item{
			{Label: "// BOARD", Active: true},
			{Label: "// TABLE"},
			{Label: "// GRAPH"},
		}
	}
	switch propLabel(d.props, "state") {
	case "home":
		opts.Home = true
		opts.Segment = "home"
		opts.SegmentHint = "select a project"
	case "overlay":
		opts.Overlay = true
	case "compact":
		opts.Width = 48
	}
	return styles, opts
}

func (d headerDemo) View(c demoCtx) string {
	styles, opts := d.opts(c)
	return header.Render(styles, opts)
}

func (d headerDemo) Status(c demoCtx) string {
	styles, opts := d.opts(c)
	return fmt.Sprintf("state %s · Height %d · Width reports %d",
		propLabel(d.props, "state"), header.Height(styles, opts), header.Width(styles, opts))
}

func (d headerDemo) Props() []prop { return d.props }

func (d headerDemo) SetProp(p prop, c demoCtx) demo {
	d.props = replaceProp(d.props, p)
	return d.Resize(c)
}

func (d headerDemo) Help() []key.Binding { return statelessHelp() }

// ---- chipstrip --------------------------------------------------------------

type chipstripDemo struct {
	props []prop
}

func newChipstripDemo(demoCtx) demo {
	return chipstripDemo{props: []prop{
		choiceProp("kind", "Logs filter chips, or the Stats period strip", 0, "logs filter", "stats period"),
		choiceProp("active", "which chip owns the accent", 0, "first", "second", "third", "fourth"),
		autoProp("width", "overflow budget; auto follows the frame", 200),
	}}
}

func (d chipstripDemo) Resize(demoCtx) demo             { return d }
func (d chipstripDemo) Update(tea.KeyMsg, demoCtx) demo { return d }

func (d chipstripDemo) width(c demoCtx) int {
	return maxInt(minInt(propAuto(d.props, "width", c.kit.Width-2), c.kit.Width-2), 8)
}

func (d chipstripDemo) View(c demoCtx) string {
	active := propOption(d.props, "active")
	if propLabel(d.props, "kind") == "stats period" {
		labels := []string{"7d", "30d", "all"}
		if active >= len(labels) {
			active = len(labels) - 1
		}
		chips := make([]tokenstrip.Chip, len(labels))
		for i, l := range labels {
			chips[i] = tokenstrip.Chip{Label: l, Active: i == active}
		}
		return tokenstrip.Chips(chips, tokenstrip.ChipStyles{
			Active:   c.kit.Styles.ActiveNav,
			Inactive: c.kit.Styles.Nav,
			Sep:      c.kit.Styles.Hint,
		}, tokenstrip.ChipOptions{Sep: " · ", BracketActive: false, Width: d.width(c)})
	}
	labels := []string{"all", "tool-calls", "domain", "system"}
	if active >= len(labels) {
		active = len(labels) - 1
	}
	chips := make([]tokenstrip.Chip, len(labels))
	for i, l := range labels {
		chips[i] = tokenstrip.Chip{Label: l, Active: i == active}
	}
	return tokenstrip.Chips(chips, tokenstrip.ChipStyles{
		Kicker:   c.kit.Styles.Info,
		Active:   c.kit.Styles.HintAccent,
		Inactive: c.kit.Styles.Hint,
		Hint:     c.kit.Styles.Hint,
	}, tokenstrip.ChipOptions{
		Kicker: "// FILTER:", Hint: "(F cycle)", BracketActive: true, Width: d.width(c),
	})
}

func (d chipstripDemo) Status(c demoCtx) string {
	out := d.View(c)
	return fmt.Sprintf("painted %d cols into budget %d", lipgloss.Width(out), d.width(c))
}

func (d chipstripDemo) Props() []prop { return d.props }

func (d chipstripDemo) SetProp(p prop, c demoCtx) demo {
	d.props = replaceProp(d.props, p)
	return d.Resize(c)
}

func (d chipstripDemo) Help() []key.Binding { return statelessHelp() }
