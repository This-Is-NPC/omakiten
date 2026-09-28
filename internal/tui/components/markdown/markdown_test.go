package markdown

import (
	"strings"
	"sync"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/testutil"
)

var themeOmakiten = Tokens{
	ThemeKey:   "omakiten",
	Foreground: "#E5E2E1",
	Border:     "#494543",
	Primary:    "#39FF14",
	Secondary:  "#8FAE9A",
}

var themeAlt = Tokens{
	ThemeKey:   "alt",
	Foreground: "#FFFFFF",
	Border:     "#222222",
	Primary:    "#FF00FF",
	Secondary:  "#00FFFF",
}

const sampleBody = `## Heading

Paragraph with **strong** and *emph*.

- bullet one
- bullet two

` + "```" + `
code block
` + "```" + `

> quoted line

---
`

func TestRenderEmitsTrueColorFromPinnedProfile(t *testing.T) {
	out := New(themeOmakiten).Render(sampleBody, 80)
	if out == "" {
		t.Fatal("expected non-empty rendered output")
	}
	if !strings.Contains(out, "\x1b[") {
		t.Fatal("expected ANSI escapes in rendered output")
	}
	// termenv quantises #39FF14 to RGB(56,255,20).
	if !strings.Contains(out, "38;2;56;255;20") {
		t.Errorf("expected primary color sequence for #39FF14, got:\n%s", out)
	}
	if !strings.Contains(out, "code block") {
		t.Errorf("expected code block content preserved, got:\n%s", out)
	}
}

func TestEmptyAndNilSafe(t *testing.T) {
	var nilR *Renderer
	if got := nilR.Render("anything", 80); got != "anything" {
		t.Errorf("nil receiver: expected raw passthrough, got %q", got)
	}
	r := New(themeOmakiten)
	if got := r.Render("", 80); got != "" {
		t.Errorf("empty body: expected empty string, got %q", got)
	}
	if got := r.Render("   \n\n", 80); strings.TrimSpace(got) != "" {
		t.Errorf("whitespace body: expected blank, got %q", got)
	}
}

func TestBodyHonoursToggle(t *testing.T) {
	body := "## Heading\n\ntext"
	r := New(themeOmakiten)
	if got := Body(r, body, 80, false); got != strings.TrimRight(body, "\n") {
		t.Errorf("toggle off should return raw body, got %q", got)
	}
	rendered := Body(r, body, 80, true)
	if rendered == body || !strings.Contains(rendered, "\x1b[") {
		t.Errorf("toggle on should return ANSI-styled output, got %q", rendered)
	}
}

func TestBodySanitizesRawAndRenderedFixtures(t *testing.T) {
	r := New(themeOmakiten)
	cases := map[string]string{
		"c0":      "## C0\x00 heading\n\nUnicode: 漢字 👋\ttext",
		"esc-csi": "## ESC\x1b[31m red\x1b[0m 漢字 👋\n\n**bold** stays Markdown",
		"osc":     "## OSC\x1b]0;owned title\a 漢字 👋\n\n- safe bullet",
		"c1":      "## C1\u009b31m red\u009dtitle\u009c 漢字 👋\n\n> quoted",
		"clean":   "## Unicode 漢字 👋\n\n**bold**, *italic*, and\nline structure",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			raw := Body(r, body, 80, false)
			rendered := Body(r, body, 80, true)
			testutil.Golden(t, "markdown/"+name+".raw.golden", raw)
			testutil.Golden(t, "markdown/"+name+".rendered.golden", trimFixtureLineEnds(ansi.Strip(rendered)))
			assertSafeMarkdown(t, raw)
			assertSafeMarkdown(t, ansi.Strip(rendered))
		})
	}
}

func trimFixtureLineEnds(body string) string {
	lines := strings.Split(body, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	return strings.Join(lines, "\n")
}

func TestRenderSanitizesDirectInputAndToggleNeverLeaksControls(t *testing.T) {
	const hostile = "# safe\x1b[31m red\x1b]0;title\a\x00\u009b31m\n\n漢字 👋"
	r := New(themeOmakiten)
	for _, rendered := range []bool{false, true, false, true} {
		out := Body(r, hostile, 80, rendered)
		assertSafeMarkdown(t, ansi.Strip(out))
	}
	assertSafeMarkdown(t, ansi.Strip(r.Render(hostile, 80)))
	assertSafeMarkdown(t, Body(nil, hostile, 80, false))
}

func TestMarkdownRejectsRawInvalidUTF8TerminalBytes(t *testing.T) {
	raw := string([]byte("# safe \x9downed\x9c 漢字 👋\xff"))
	r := New(themeOmakiten)

	for _, rendered := range []bool{false, true} {
		out := Body(r, raw, 80, rendered)
		plain := ansi.Strip(out)
		if !utf8.ValidString(plain) {
			t.Fatalf("rendered=%v retained invalid UTF-8: %q", rendered, plain)
		}
		if strings.Contains(plain, "owned") {
			t.Fatalf("rendered=%v retained raw OSC payload: %q", rendered, plain)
		}
		if !strings.Contains(plain, "safe") || !strings.Contains(plain, "漢字 👋") {
			t.Fatalf("rendered=%v lost printable Unicode: %q", rendered, plain)
		}
	}
}

func assertSafeMarkdown(t *testing.T, body string) {
	t.Helper()
	if strings.IndexFunc(body, func(r rune) bool { return r != '\n' && unicode.IsControl(r) }) >= 0 {
		t.Fatalf("markdown output retained a terminal control: %q", body)
	}
	if !strings.Contains(body, "漢字 👋") {
		t.Fatalf("markdown output lost harmless Unicode: %q", body)
	}
}

func TestLinesMatchesBody(t *testing.T) {
	r := New(themeOmakiten)
	body := sampleBody
	out := Body(r, body, 40, true)
	want := strings.Count(out, "\n") + 1
	if out == "" {
		want = 0
	}
	if got := Lines(r, body, 40, true); got != want {
		t.Fatalf("Lines = %d, want %d (must match Body)", got, want)
	}
}

func TestCachedAcrossCalls(t *testing.T) {
	r := New(themeOmakiten)
	first := r.Render(sampleBody, 80)
	if got := len(r.cache); got != 1 {
		t.Fatalf("expected 1 cache entry after first render, got %d", got)
	}
	second := r.Render(sampleBody, 80)
	if first != second {
		t.Errorf("cache hit returned different output")
	}
	r.Render(sampleBody, 40)
	if got := len(r.cache); got != 2 {
		t.Errorf("expected 2 cache entries after width change, got %d", got)
	}
}

func TestThemeChangeRebuildsColors(t *testing.T) {
	out1 := New(themeOmakiten).Render(sampleBody, 80)
	out2 := New(themeAlt).Render(sampleBody, 80)
	if out1 == out2 {
		t.Fatal("expected different output across themes")
	}
	if !strings.Contains(out1, "38;2;56;255;20") {
		t.Errorf("theme1 should carry omakiten primary")
	}
	if !strings.Contains(out2, "38;2;255;0;255") {
		t.Errorf("theme2 should carry alt primary")
	}
}

func TestLRUEvictionRespectsBound(t *testing.T) {
	r := New(themeOmakiten)
	for i := 0; i <= CacheCapacity; i++ {
		r.Render("# heading "+strings.Repeat("x", i+1), 80)
	}
	if got := len(r.cache); got != CacheCapacity {
		t.Fatalf("len(cache) = %d, want %d", got, CacheCapacity)
	}
	evictedKey := cacheKey{bodyHash: hashBody("# heading " + strings.Repeat("x", 1)), width: 80}
	if _, ok := r.cache[evictedKey]; ok {
		t.Fatalf("LRU did not evict the first-inserted body")
	}
	r.Render("# heading "+strings.Repeat("x", 1), 80)
	if _, ok := r.cache[evictedKey]; !ok {
		t.Fatalf("re-render did not repopulate the cache entry")
	}
}

func TestReusesTermRendererPerWidth(t *testing.T) {
	r := New(themeOmakiten)
	r.Render("# one", 80)
	first := r.renderers[80]
	r.Render("# two", 80)
	if r.renderers[80] != first {
		t.Fatalf("second render at width 80 allocated a new TermRenderer")
	}
}

func TestReloadClearsAllCaches(t *testing.T) {
	r := New(themeOmakiten)
	r.Render("# warm", 80)
	r.Reload(themeAlt)
	if len(r.cache) != 0 || len(r.renderers) != 0 {
		t.Fatalf("Reload did not clear caches")
	}
	out := r.Render("# fresh", 80)
	if !strings.Contains(out, "38;2;255;0;255") {
		t.Fatalf("post-reload render did not pick up alt theme primary")
	}
}

func TestConcurrentRenderMatchesSequential(t *testing.T) {
	bodies := []string{
		"# Heading one\n\nFirst paragraph with **bold** text.",
		"# Heading two\n\nSecond paragraph with *italic* text.",
		"## Section\n\n- bullet one\n- bullet two\n- bullet three",
		"Plain prose body without any decoration whatsoever.",
		"> Block quote with `inline code` inside.",
	}
	const width = 60
	const goroutines = 32

	baseline := make(map[string]string, len(bodies))
	for _, body := range bodies {
		baseline[body] = New(Tokens{ThemeKey: "test"}).Render(body, width)
	}

	r := New(Tokens{ThemeKey: "test"})
	var wg sync.WaitGroup
	results := make([]string, goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		body := bodies[i%len(bodies)]
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = r.Render(body, width)
		}()
	}
	wg.Wait()

	for i, got := range results {
		body := bodies[i%len(bodies)]
		if got != baseline[body] {
			t.Fatalf("goroutine %d concurrent render mismatch", i)
		}
	}
}

func TestNewIsIndependent(t *testing.T) {
	a := New(themeOmakiten)
	b := New(themeOmakiten)
	if a == b {
		t.Fatal("New must return distinct Renderers")
	}
	a.Render(sampleBody, 80)
	if len(a.cache) == 0 {
		t.Fatal("first renderer should have cached the render")
	}
	if len(b.cache) != 0 {
		t.Fatal("second New must not share the first renderer's cache")
	}
	b.Render(sampleBody, 40)
	if len(a.cache) != 1 {
		t.Fatalf("second renderer must not mutate the first cache, len=%d", len(a.cache))
	}
}

func TestReloadSameTokensKeepsCache(t *testing.T) {
	r := New(themeOmakiten)
	r.Render(sampleBody, 80)
	r.Reload(themeOmakiten)
	if len(r.cache) != 1 {
		t.Fatalf("same-token Reload dropped the cache, len=%d", len(r.cache))
	}
}
