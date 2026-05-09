package tui

import (
	"strings"
	"testing"

	"omakiten/internal/config"
)

// markdownThemeOmakiten mirrors the bundled omakiten palette tokens that
// the renderer consumes. Pinned in the test so colour changes there are
// caught by the assertions instead of silently re-coloring the body.
var markdownThemeOmakiten = config.Theme{
	Key: "omakiten",
	Colors: map[string]string{
		"foreground": "#E5E2E1",
		"border":     "#494543",
		"primary":    "#39FF14",
		"secondary":  "#8FAE9A",
	},
}

var markdownThemeAlt = config.Theme{
	Key: "alt",
	Colors: map[string]string{
		"foreground": "#FFFFFF",
		"border":     "#222222",
		"primary":    "#FF00FF",
		"secondary":  "#00FFFF",
	},
}

const markdownSampleBody = `## Heading

Paragraph with **strong** and *emph*.

- bullet one
- bullet two

` + "```" + `
code block
` + "```" + `

> quoted line

---
`

func TestMarkdownRenderer_RendersSampleBody(t *testing.T) {
	r := newMarkdownRenderer(tokensFromTheme(markdownThemeOmakiten))
	out := r.Render(markdownSampleBody, 80)

	if out == "" {
		t.Fatal("expected non-empty rendered output")
	}
	if !strings.Contains(out, "\x1b[") {
		t.Fatal("expected ANSI escapes in rendered output")
	}
	// Heading should be coloured with the primary token via a 24-bit fg
	// sequence. termenv quantises `#39FF14` to RGB(56,255,20) — checked
	// against the actual TrueColor encoder to keep the assertion stable.
	if !strings.Contains(out, "38;2;56;255;20") {
		t.Errorf("expected primary color sequence for #39FF14, got:\n%s", out)
	}
	if !strings.Contains(out, "code block") {
		t.Errorf("expected code block content preserved, got:\n%s", out)
	}
}

func TestMarkdownRenderer_EmptyAndNilSafe(t *testing.T) {
	var nilR *markdownRenderer
	if got := nilR.Render("anything", 80); got != "anything" {
		t.Errorf("nil receiver: expected raw passthrough, got %q", got)
	}
	r := newMarkdownRenderer(tokensFromTheme(markdownThemeOmakiten))
	if got := r.Render("", 80); got != "" {
		t.Errorf("empty body: expected empty string, got %q", got)
	}
	if got := r.Render("   \n\n", 80); strings.TrimSpace(got) != "" {
		t.Errorf("whitespace body: expected blank, got %q", got)
	}
}

func TestMarkdownRenderer_CachedAcrossCalls(t *testing.T) {
	r := newMarkdownRenderer(tokensFromTheme(markdownThemeOmakiten))
	first := r.Render(markdownSampleBody, 80)
	if got := len(r.cache); got != 1 {
		t.Fatalf("expected 1 cache entry after first render, got %d", got)
	}
	second := r.Render(markdownSampleBody, 80)
	if first != second {
		t.Errorf("cache hit returned different output:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	// Different width = different cache slot.
	r.Render(markdownSampleBody, 40)
	if got := len(r.cache); got != 2 {
		t.Errorf("expected 2 cache entries after width change, got %d", got)
	}
}

func TestMarkdownRenderer_ThemeChangeRebuildsColors(t *testing.T) {
	r1 := newMarkdownRenderer(tokensFromTheme(markdownThemeOmakiten))
	r2 := newMarkdownRenderer(tokensFromTheme(markdownThemeAlt))

	out1 := r1.Render(markdownSampleBody, 80)
	out2 := r2.Render(markdownSampleBody, 80)

	if out1 == out2 {
		t.Fatal("expected different output across themes — got identical strings")
	}
	if !strings.Contains(out1, "38;2;56;255;20") {
		t.Errorf("theme1 should carry omakiten primary (#39FF14 → 56;255;20)")
	}
	if !strings.Contains(out2, "38;2;255;0;255") {
		t.Errorf("theme2 should carry alt primary (#FF00FF → 255;0;255)")
	}
}

func TestRenderBodyMarkdown_HonorsToggle(t *testing.T) {
	body := "## Heading\n\ntext"
	m := Model{
		styles:           newStyles(markdownThemeOmakiten),
		theme:            markdownThemeOmakiten,
		markdown:         newMarkdownRenderer(tokensFromTheme(markdownThemeOmakiten)),
		markdownRendered: false,
	}
	if got := m.renderBodyMarkdown(body, 80); got != strings.TrimRight(body, "\n") {
		t.Errorf("toggle off should return raw body, got %q", got)
	}
	m.markdownRendered = true
	rendered := m.renderBodyMarkdown(body, 80)
	if rendered == body || !strings.Contains(rendered, "\x1b[") {
		t.Errorf("toggle on should return ANSI-styled output, got %q", rendered)
	}
}

func TestRenderBodyMarkdown_EmptyShortCircuits(t *testing.T) {
	m := Model{
		styles:           newStyles(markdownThemeOmakiten),
		markdown:         newMarkdownRenderer(tokensFromTheme(markdownThemeOmakiten)),
		markdownRendered: true,
	}
	if got := m.renderBodyMarkdown("", 80); got != "" {
		t.Errorf("empty body: expected empty string, got %q", got)
	}
	if got := m.renderBodyMarkdown("   ", 80); strings.TrimSpace(got) != "" {
		t.Errorf("whitespace body: expected blank, got %q", got)
	}
}

func TestToggleMarkdownRendered_FlipsAndStatus(t *testing.T) {
	m := Model{markdownRendered: true}
	m.toggleMarkdownRendered()
	if m.markdownRendered {
		t.Error("expected markdownRendered=false after toggle")
	}
	if m.status != "Markdown raw" {
		t.Errorf("expected status 'Markdown raw', got %q", m.status)
	}
	m.toggleMarkdownRendered()
	if !m.markdownRendered {
		t.Error("expected markdownRendered=true after second toggle")
	}
	if m.status != "Markdown rendered" {
		t.Errorf("expected status 'Markdown rendered', got %q", m.status)
	}
}
