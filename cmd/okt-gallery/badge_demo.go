package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/tokenstrip"
)

// badgeLabels is the demo's locale pack. The gallery has no catalog, and
// screenkit.Kit answers with the raw key when it has no resolver — which would
// put `tui.badge.blockers` on a pill and hide the only thing worth looking at,
// the WIDTH each pill occupies once it is styled.
var badgeLabels = map[string]string{
	"tui.badge.blocker":    "blocker",
	"tui.badge.blockers":   "blockers",
	"tui.badge.comment":    "comment",
	"tui.badge.comments":   "comments",
	"tui.badge.subtask":    "subtask",
	"tui.badge.subtasks":   "subtasks",
	"tui.badge.tokens_fmt": "%d tok",
	"tui.badge.active":     "ACTIVE",
	"tui.badge.custom":     "CUSTOM",
	"tui.badge.fix":        "FIX",
}

func badgeText(key string) string {
	if label, ok := badgeLabels[key]; ok {
		return label
	}
	return key
}

type badgeDemo struct {
	props []prop
}

func newBadgeDemo(demoCtx) demo {
	return badgeDemo{props: []prop{
		choiceProp("priority", "config.priorities[].color — the token, not a style", 0,
			"error", "warning", "success", "info", "none"),
		numberProp("blockers", "the blocker count pill", 2, 0, 99),
		numberProp("comments", "the comment count pill", 1, 0, 99),
		numberProp("subtasks", "the sub-task count pill", 0, 0, 99),
		numberProp("tags", "how many #tag pills follow the counts", 3, 0, 12),
		choiceProp("entity", "which entity family's trailing markers to append", 4,
			"law", "persona", "skill", "template", "none"),
		numberProp("tokens", "the token spend the band is picked from", 1800, 0, 9000),
		numberProp("token warn", "config.TokenBadgeThresholds — green above this goes yellow", 1500, 0, 9000),
		numberProp("token alarm", "…and yellow above this goes red", 4000, 0, 9000),
		autoProp("width", "the budget handed to Wrap — auto follows the frame", 200),
	}}
}

func (d badgeDemo) Resize(demoCtx) demo             { return d }
func (d badgeDemo) Update(tea.KeyMsg, demoCtx) demo { return d }

// budget is the width the packer is given. Auto follows the frame, which is what
// a card does — its badge line gets the card's inner width.
func (d badgeDemo) budget(c demoCtx) int {
	return maxInt(minInt(propAuto(d.props, "width", c.kit.Width), c.kit.Width), 1)
}

func (d badgeDemo) badges(c demoCtx) []string {
	s := c.kit.Styles
	var out []string

	if color := propLabel(d.props, "priority"); color != "none" {
		// The label is the config VALUE; the demo reuses the colour token as the
		// label so the two stay legible together on the pill.
		out = append(out, tokenstrip.Pill(s, color, color))
	}

	out = append(out, tokenstrip.Counts{
		Blockers: propNumber_(d.props, "blockers"),
		Comments: propNumber_(d.props, "comments"),
		Subtasks: propNumber_(d.props, "subtasks"),
	}.Render(s, badgeText)...)

	for i := 0; i < propNumber_(d.props, "tags"); i++ {
		out = append(out, tokenstrip.Tag(s, sampleTagLabels[i%len(sampleTagLabels)]))
	}

	if family := propLabel(d.props, "entity"); family != "none" {
		out = append(out, tokenstrip.Spend(s, badgeText,
			propNumber_(d.props, "tokens"),
			propNumber_(d.props, "token warn"),
			propNumber_(d.props, "token alarm")))
		if family == "law" {
			out = append(out, tokenstrip.Scope(s, "GLOBAL"))
		}
		out = append(out, tokenstrip.Fix(s, badgeText), tokenstrip.Active(s, badgeText), tokenstrip.Custom(s, badgeText))
	}
	return out
}

func (d badgeDemo) View(c demoCtx) string {
	width := d.budget(c)
	badges := d.badges(c)

	// The rule marks the budget. A pill that crosses it is overflow you can see
	// without counting columns — which is the only reason the packer exists.
	rule := c.kit.HRule(width)
	body := tokenstrip.Pills(badges, width)
	if body == "" {
		body = c.kit.Styles.Hint.Render(screenkit.WrapAt("no badges at this configuration", width))
	}
	return strings.Join([]string{
		c.kit.Styles.KickerCount("badges", len(badges)),
		rule,
		body,
		rule,
	}, "\n")
}

func (d badgeDemo) Status(c demoCtx) string {
	width := d.budget(c)
	badges := d.badges(c)

	// Lines and Wrap walking the same packer is the package's whole claim, so
	// the status reports both numbers rather than the one it trusts.
	counted := tokenstrip.PillRows(badges, width)
	drawn := 0
	if rendered := tokenstrip.Pills(badges, width); rendered != "" {
		drawn = strings.Count(rendered, "\n") + 1
	}
	agreement := fmt.Sprintf("%d %s", drawn, pluralRows(drawn))
	if counted != drawn {
		agreement = fmt.Sprintf("Lines says %d but Wrap drew %d — THE MEASURER IS WRONG", counted, drawn)
	}

	widest := 0
	for _, b := range badges {
		widest = maxInt(widest, lipgloss.Width(b))
	}
	overflow := ""
	if widest > width {
		overflow = fmt.Sprintf(" · widest pill is %d, past the %d budget — it gets its own row uncut", widest, width)
	}
	return fmt.Sprintf("%d badges packed into %d columns → %s%s", len(badges), width, agreement, overflow)
}

func (d badgeDemo) Props() []prop { return d.props }

func (d badgeDemo) SetProp(p prop, c demoCtx) demo {
	d.props = replaceProp(d.props, p)
	return d.Resize(c)
}

func (d badgeDemo) Help() []key.Binding { return statelessHelp() }

func pluralRows(n int) string {
	if n == 1 {
		return "row"
	}
	return "rows"
}

// sampleTagLabels are deliberately uneven in length: a packer only shows its
// policy when the next pill sometimes fits and sometimes does not.
var sampleTagLabels = []string{"tui", "layout", "arch", "i18n", "regression", "ui", "perf", "docs"}
