package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/overlay"
	"omakiten/internal/tui/components/tokenstrip"
)

// overlayDemo is the three surfaces package overlay owns, selected by shape:
// help, card, place. Not one tea.Model.
type overlayDemo struct {
	props []prop
}

func newOverlayDemo(demoCtx) demo {
	return overlayDemo{props: []prop{
		choiceProp("shape", "help · card · place — three surfaces, one package", 0, "help", "card", "place"),
		choiceProp("scope", "help: current surface only, or every group", 0, "current", "all"),
		numberProp("viewport", "help: scroll window (0 = paint everything)", 12, 0, 40),
		numberProp("scroll", "help: offset into the body", 0, 0, 40),
		choiceProp("footer", "card: show the dismiss/action footer", 0, "yes", "no"),
		textProp("text", "card: already-typed bubble body", "Confirm delete of project demo?\nThis cannot be undone."),
		choiceProp("anchor", "place: which of the nine splices", 0,
			"top-left", "top-center", "top-right",
			"middle-left", "center", "middle-right",
			"bottom-left", "bottom-center", "bottom-right"),
	}}
}

func (d overlayDemo) shape() string { return propLabel(d.props, "shape") }

func (d overlayDemo) Resize(demoCtx) demo             { return d }
func (d overlayDemo) Update(tea.KeyMsg, demoCtx) demo { return d }

func (d overlayDemo) View(c demoCtx) string {
	switch d.shape() {
	case "card":
		return overlay.RenderCard(d.cardSpec(c))
	case "place":
		return d.placeView(c)
	default:
		return overlay.Render(d.helpStyles(c), d.helpOpts(c))
	}
}

func (d overlayDemo) Status(c demoCtx) string {
	switch d.shape() {
	case "card":
		spec := d.cardSpec(c)
		return fmt.Sprintf("card %d×%d · auto-height %v · footer %d cells",
			spec.Width, spec.Height, spec.AutoHeight, lipgloss.Width(spec.Footer))
	case "place":
		return fmt.Sprintf("place splice at %s", propLabel(d.props, "anchor"))
	default:
		styles := d.helpStyles(c)
		opts := d.helpOpts(c)
		footer := "press ? to close"
		return fmt.Sprintf("body %d rows · ViewportRows(24, header2, footer%d) = %d · FooterHeight %d",
			len(overlay.Lines(styles, opts)),
			overlay.FooterHeight(styles, footer),
			overlay.ViewportRows(24, 2, overlay.FooterHeight(styles, footer)),
			overlay.FooterHeight(styles, footer))
	}
}

func (d overlayDemo) Props() []prop { return d.props }

func (d overlayDemo) SetProp(p prop, c demoCtx) demo {
	d.props = replaceProp(d.props, p)
	return d.Resize(c)
}

func (d overlayDemo) Help() []key.Binding { return statelessHelp() }

func (d overlayDemo) helpStyles(c demoCtx) overlay.Styles {
	return overlay.Styles{
		Info:      c.kit.Styles.Info,
		Hint:      c.kit.Styles.Hint,
		Key:       c.kit.Styles.HintAccent,
		Footer:    c.kit.Styles.Hint,
		Separator: c.kit.Styles.Separator,
	}
}

func (d overlayDemo) helpOpts(c demoCtx) overlay.Options {
	groups := []overlay.Group{
		{Title: "global", Bindings: []overlay.Binding{
			{Key: "?", Desc: "close help"},
			{Key: "a", Desc: "toggle all / current"},
			{Key: "q · ctrl+c", Desc: "quit"},
		}},
		{Title: "board", Bindings: []overlay.Binding{
			{Key: "↑ ↓ · j k", Desc: "move the cursor"},
			{Key: "enter", Desc: "open the focused card"},
			{Key: "n", Desc: "new task"},
			{Key: "m", Desc: "move across buckets"},
		}},
	}
	if propLabel(d.props, "scope") == "all" {
		groups = append(groups, overlay.Group{Title: "settings", Bindings: []overlay.Binding{
			{Key: "e", Desc: "edit in $EDITOR"},
			{Key: "d · d", Desc: "arm delete"},
		}})
	}
	title := "help · current"
	if propLabel(d.props, "scope") == "all" {
		title = "help · all"
	}
	return overlay.Options{
		Title: title, ScopeHint: "a toggles scope", Groups: groups,
		Scroll: propNumber_(d.props, "scroll"), Viewport: propNumber_(d.props, "viewport"),
		FormatScrollHint: func(above, below int) string {
			if above == 0 && below == 0 {
				return ""
			}
			return c.kit.Styles.Hint.Render(fmt.Sprintf("▲ %d above · ▼ %d below", above, below))
		},
	}
}

func (d overlayDemo) cardSpec(c demoCtx) overlay.Card {
	const width = 36
	frameWidth := width - 2
	footer := ""
	if propLabel(d.props, "footer") != "no" {
		styles := tokenstrip.FromStyles(c.kit.Styles)
		footer = tokenstrip.KeysWrapped([]tokenstrip.Key{
			{Key: "esc", Label: "close", Primary: true},
		}, styles, frameWidth)
	}
	return overlay.Card{
		Width:            width,
		Height:           10,
		AutoHeight:       true,
		BorderVisible:    true,
		BorderForeground: lipgloss.Color("#ffffff"),
		Style:            overlay.StyleRounded,
		Padding:          overlay.Padding{Right: 1, Left: 1},
		TailSide:         overlay.TailBottom,
		Frames:           []string{"!", "?"},
		Text:             propString(d.props, "text"),
		Footer:           footer,
	}
}

func (d overlayDemo) placeView(c demoCtx) string {
	width := c.kit.Width
	if width < 8 {
		width = 8
	}
	height := c.kit.Height
	if height < 4 {
		height = 4
	}
	row := strings.Repeat(".", width)
	base := strings.TrimRight(strings.Repeat(row+"\n", height), "\n")
	return overlay.Overlay(base, "AB\nCD", overlay.Position(propLabel(d.props, "anchor")))
}
