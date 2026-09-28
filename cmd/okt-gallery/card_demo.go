package main

import (
	"fmt"
	"omakiten/internal/tui/components/tokenstrip"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/card"
)

type cardDemo struct {
	props []prop
}

func newCardDemo(demoCtx) demo {
	return cardDemo{props: []prop{
		choiceProp("kind", "kanban Spec, a comment body, or a system event — one package, several types", 0,
			"kanban", "comment", "system"),
		numberProp("id", "prefixes the title as `#id `; 0 drops the prefix", 412, 0, 99999),
		textProp("title", "wraps inside the content width, hanging under the prefix",
			"resolve the layout exactly once per keystroke"),
		choiceProp("state", "which border variant the card wears", 0,
			"idle", "selected", "archived", "accent"),
		choiceProp("cursor", "the `›` glyph — separate from selection on purpose", 1, "yes", "no"),
		numberProp("meta", "rows between the title and the badges, each truncated", 0, 0, 4),
		numberProp("badges", "pre-rendered pills packed onto the badge line", 3, 0, 8),
		autoProp("box width", "the width handed to the box style — auto follows the frame", 200),
		textProp("author", "the attribution, or the event sentence for a system card", "user"),
		textProp("timestamp", "follows the author after a middot; blank drops the middot too", "2026-04-11 08:14"),
		numberProp("body lines", "how much comment there is to fold", 3, 0, 40),
		numberProp("line limit", "rows shown before folding; 0 uses the default, -1 never folds",
			0, -1, 40),
		numberProp("tags", "pills under the body — comment cards only", 2, 0, 8),
		choiceProp("focused", "the feed's cursor tints the border", 1, "yes", "no"),
		autoProp("width", "the feed-card box width — auto follows the frame", 200),
	}}
}

func (d cardDemo) kind() string { return propLabel(d.props, "kind") }

func (d cardDemo) Resize(demoCtx) demo             { return d }
func (d cardDemo) Update(tea.KeyMsg, demoCtx) demo { return d }

func (d cardDemo) boxWidth(c demoCtx) int {
	return maxInt(minInt(propAuto(d.props, "box width", c.kit.Width-2), c.kit.Width-2), 6)
}

func (d cardDemo) feedWidth(c demoCtx) int {
	return maxInt(minInt(propAuto(d.props, "width", c.kit.Width-2), c.kit.Width-2), 10)
}

func (d cardDemo) spec(c demoCtx) card.Spec {
	box := d.boxWidth(c)
	state := propLabel(d.props, "state")
	return card.Spec{
		ID:         int64(propNumber_(d.props, "id")),
		Title:      propString(d.props, "title"),
		Meta:       d.meta(c),
		Badges:     d.badges(c),
		Selected:   state == "selected",
		Archived:   state == "archived",
		Accent:     state == "accent",
		Cursor:     propLabel(d.props, "cursor") == "yes",
		BoxWidth:   box,
		InnerWidth: maxInt(box-2, 1),
	}
}

func (d cardDemo) meta(c demoCtx) []string {
	source := []string{"@howl", "wave 2 · components/card", "updated 2 hours ago", "/home/howl/Projects/person/omakiten"}
	n := clamp(propNumber_(d.props, "meta"), 0, len(source))
	out := make([]string, n)
	for i := range out {
		out[i] = c.kit.Styles.Hint.Render(source[i])
	}
	return out
}

func (d cardDemo) badges(c demoCtx) []string {
	return samplePills(c, propNumber_(d.props, "badges"))
}

func (d cardDemo) commentBody() string {
	n := propNumber_(d.props, "body lines")
	lines := make([]string, 0, n)
	for i := 0; i < n; i++ {
		lines = append(lines, fmt.Sprintf("line %d of the comment body, long enough to wrap on a narrow feed", i+1))
	}
	return strings.Join(lines, "\n")
}

func (d cardDemo) commentTags(c demoCtx) []string {
	n := propNumber_(d.props, "tags")
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, tokenstrip.Tag(c.kit.Styles, sampleTagLabels[i%len(sampleTagLabels)]))
	}
	return out
}

func (d cardDemo) View(c demoCtx) string {
	painter := card.Painter{Styles: c.kit.Styles}
	switch d.kind() {
	case "system":
		return painter.System(card.System{
			Label:     propString(d.props, "author"),
			Timestamp: propString(d.props, "timestamp"),
			Focused:   propLabel(d.props, "focused") == "yes",
			Width:     d.feedWidth(c),
		})
	case "comment":
		return painter.Comment(card.Comment{
			Author:    propString(d.props, "author"),
			Timestamp: propString(d.props, "timestamp"),
			Body:      d.commentBody(),
			EmptyText: "no comment body",
			Tags:      d.commentTags(c),
			LineLimit: propNumber_(d.props, "line limit"),
			Focused:   propLabel(d.props, "focused") == "yes",
			Width:     d.feedWidth(c),
		})
	default:
		return painter.Render(d.spec(c))
	}
}

func (d cardDemo) Status(c demoCtx) string {
	if d.kind() != "kanban" {
		return d.feedStatus(c)
	}
	painter := card.Painter{Styles: c.kit.Styles}
	spec := d.spec(c)

	rendered := painter.Render(spec)
	drawn := strings.Count(rendered, "\n") + 1
	agreement := fmt.Sprintf("%d rows", drawn)
	if measured := painter.Height(spec); measured != drawn {
		agreement = fmt.Sprintf("Height says %d but Render drew %d — THE MEASURER IS WRONG", measured, drawn)
	}

	widest := 0
	for _, row := range strings.Split(rendered, "\n") {
		widest = maxInt(widest, lipgloss.Width(row))
	}
	return fmt.Sprintf("box %d + 2 border = %d columns · %s · a lane budgeting %d rows fits %d of these",
		spec.BoxWidth, widest, agreement, c.kit.Height, c.kit.Height/maxInt(drawn, 1))
}

func (d cardDemo) feedStatus(c demoCtx) string {
	width := d.feedWidth(c)
	rendered := d.View(c)
	rows := strings.Count(rendered, "\n") + 1

	widest := 0
	for _, row := range strings.Split(rendered, "\n") {
		widest = maxInt(widest, lipgloss.Width(row))
	}
	folded := ""
	if d.kind() == "comment" {
		if full := strings.Count(d.unfolded(c), "\n") + 1; full > rows {
			folded = fmt.Sprintf(" · folded: %d of %d body rows hidden", full-rows+1, full-2)
		}
	}
	return fmt.Sprintf("box %d + 2 border = %d columns · %d rows · %d for content%s",
		width, widest, rows, card.ContentWidth(width), folded)
}

func (d cardDemo) unfolded(c demoCtx) string {
	return card.Painter{Styles: c.kit.Styles}.Comment(card.Comment{
		Author:    propString(d.props, "author"),
		Timestamp: propString(d.props, "timestamp"),
		Body:      d.commentBody(),
		EmptyText: "no comment body",
		Tags:      d.commentTags(c),
		LineLimit: -1,
		Width:     d.feedWidth(c),
	})
}

func (d cardDemo) Props() []prop { return d.props }

func (d cardDemo) SetProp(p prop, c demoCtx) demo {
	d.props = replaceProp(d.props, p)
	return d.Resize(c)
}

func (d cardDemo) Help() []key.Binding { return statelessHelp() }
