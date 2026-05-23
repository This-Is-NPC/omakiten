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

// TestMarkdownRenderer_LRUEvictionRespectsBound pins the bounded-cache
// contract: filling the cache past markdownCacheCapacity evicts the
// oldest entry; re-rendering the freshly-evicted body adds a new
// entry and pushes the next-oldest out. Without the bound, long
// sessions that scroll through every entity body grew the cache
// unbounded.
func TestMarkdownRenderer_LRUEvictionRespectsBound(t *testing.T) {
	r := newMarkdownRenderer(tokensFromTheme(markdownThemeOmakiten))
	// Render markdownCacheCapacity + 1 distinct bodies at the same
	// width so the (bodyHash, width) keys are all unique.
	for i := 0; i <= markdownCacheCapacity; i++ {
		body := "# heading " + strings.Repeat("x", i+1)
		r.Render(body, 80)
	}
	if got := len(r.cache); got != markdownCacheCapacity {
		t.Fatalf("len(cache) = %d, want %d (LRU eviction failed)", got, markdownCacheCapacity)
	}
	if got := r.order.Len(); got != markdownCacheCapacity {
		t.Fatalf("len(order) = %d, want %d (LRU list out of sync with map)", got, markdownCacheCapacity)
	}

	// The first body inserted (i=0) should have been evicted because
	// it was the LRU when the over-capacity insert happened.
	evictedKey := markdownCacheKey{bodyHash: hashBody("# heading " + strings.Repeat("x", 1)), width: 80}
	if _, ok := r.cache[evictedKey]; ok {
		t.Fatalf("LRU did not evict the first-inserted body")
	}

	// Re-rendering the evicted body repopulates it and pushes another
	// entry out. Cache size stays at the bound.
	r.Render("# heading "+strings.Repeat("x", 1), 80)
	if got := len(r.cache); got != markdownCacheCapacity {
		t.Fatalf("len(cache) after refill = %d, want %d", got, markdownCacheCapacity)
	}
	if _, ok := r.cache[evictedKey]; !ok {
		t.Fatalf("re-render did not repopulate the cache entry")
	}
}

// TestMarkdownRenderer_ReusesTermRendererPerWidth pins the renderer
// reuse: hitting a fresh body at the same width does NOT allocate a
// new *glamour.TermRenderer. The renderers map is the source of truth
// for "have we built a renderer at this width before".
func TestMarkdownRenderer_ReusesTermRendererPerWidth(t *testing.T) {
	r := newMarkdownRenderer(tokensFromTheme(markdownThemeOmakiten))
	r.Render("# one", 80)
	first := r.renderers[80]
	r.Render("# two", 80)
	if r.renderers[80] != first {
		t.Fatalf("second render at width 80 allocated a new TermRenderer; reuse cache missed")
	}
	r.Render("# three", 120)
	if r.renderers[120] == nil {
		t.Fatalf("width 120 did not populate the renderers map")
	}
}

// TestMarkdownRenderer_ReloadThemeClearsAllCaches asserts the theme-
// rotation contract: reloadTheme rebuilds the StyleConfig and resets
// the output cache + the per-width renderer cache so the next render
// emits styles from the new palette.
func TestMarkdownRenderer_ReloadThemeClearsAllCaches(t *testing.T) {
	r := newMarkdownRenderer(tokensFromTheme(markdownThemeOmakiten))
	r.Render("# warm", 80)
	if len(r.cache) == 0 || len(r.renderers) == 0 {
		t.Fatalf("expected caches populated before reload")
	}
	r.reloadTheme(tokensFromTheme(markdownThemeAlt))
	if len(r.cache) != 0 {
		t.Fatalf("reloadTheme did not clear output cache (len=%d)", len(r.cache))
	}
	if len(r.renderers) != 0 {
		t.Fatalf("reloadTheme did not clear renderer cache (len=%d)", len(r.renderers))
	}
	out := r.Render("# fresh", 80)
	if !strings.Contains(out, "38;2;255;0;255") {
		t.Fatalf("post-reload render did not pick up alt theme primary")
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
	m := Model{markdownRendered: true, repos: Repositories{Catalog: newTestCatalog(t)}}
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
