package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/tui/components/markdown"
)

type markdownDemo struct {
	props []prop
	md    *markdown.Renderer
}

func newMarkdownDemo(demoCtx) demo {
	return markdownDemo{
		md: markdown.New(markdown.Tokens{}),
		props: []prop{
			choiceProp("mode", "rendered through glamour, or the raw source the M toggle shows", 0, "rendered", "raw"),
			choiceProp("body", "short prose, a heading+list, or a long wrapping paragraph", 0,
				"heading list", "paragraph", "code"),
			autoProp("width", "columns glamour wraps to", 200),
		},
	}
}

func (d markdownDemo) Resize(demoCtx) demo             { return d }
func (d markdownDemo) Update(tea.KeyMsg, demoCtx) demo { return d }

func (d markdownDemo) source() string {
	switch propLabel(d.props, "body") {
	case "paragraph":
		return "The project reader has to survive a paragraph long enough that it wraps at every recorded width, because the wrap column is the first thing a layout migration moves."
	case "code":
		return "## Notes\n\nInline `code` and a fence:\n\n```\nfunc main() {}\n```\n"
	default:
		return "## Heading\n\nParagraph with **strong** and *emph*.\n\n- bullet one\n- bullet two\n"
	}
}

func (d markdownDemo) tokens(c demoCtx) markdown.Tokens {
	tokens := c.kit.Markdown
	if tokens.ThemeKey == "" {
		tokens = markdown.Tokens{
			ThemeKey: "gallery", Foreground: "#CAD3F5", Border: "#494D64",
			Primary: "#8AADF4", Secondary: "#C6A0F6",
		}
	}
	return tokens
}

func (d markdownDemo) renderer(c demoCtx) *markdown.Renderer {
	d.md.Reload(d.tokens(c))
	return d.md
}

func (d markdownDemo) View(c demoCtx) string {
	width := maxInt(minInt(propAuto(d.props, "width", c.kit.Width-4), c.kit.Width-4), 20)
	rendered := propLabel(d.props, "mode") == "rendered"
	return markdown.Body(d.renderer(c), d.source(), width, rendered)
}

func (d markdownDemo) Status(c demoCtx) string {
	width := maxInt(minInt(propAuto(d.props, "width", c.kit.Width-4), c.kit.Width-4), 20)
	rendered := propLabel(d.props, "mode") == "rendered"
	r := d.renderer(c)
	lines := markdown.Lines(r, d.source(), width, rendered)
	stripped := ansi.Strip(markdown.Body(r, d.source(), width, rendered))
	return fmt.Sprintf("%s · %d rows · TrueColor forced · stripped width %d",
		propLabel(d.props, "mode"), lines, lipglossWidth(stripped))
}

func lipglossWidth(s string) int {
	w := 0
	for _, line := range strings.Split(s, "\n") {
		if n := len([]rune(line)); n > w { // approx; status only
			w = n
		}
	}
	return w
}

func (d markdownDemo) Props() []prop { return d.props }

func (d markdownDemo) SetProp(p prop, c demoCtx) demo {
	d.props = replaceProp(d.props, p)
	return d
}

func (d markdownDemo) Help() []key.Binding { return nil }
