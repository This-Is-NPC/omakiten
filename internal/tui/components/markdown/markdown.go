// Package markdown renders body fields (task descriptions, comment bodies,
// entity prose, plan goals) through glamour, themed from four colour tokens.
//
// It exists because six surfaces needed the host to paint a body: each injected
// a RenderBody / RenderMarkdown callback back into the root, so a fixture could
// not render the real output and five golden suites invented a stand-in that
// hard-wrapped lines. Those stand-ins recorded something the app does not paint.
//
// Determinism: every TermRenderer is built with termenv.TrueColor. The colour
// profile is NOT read from the environment, which is why the same body at the
// same width produces the same ANSI (and the same stripped text) under TERM=dumb
// and under a truecolor terminal. Goldens strip ANSI and record the structure
// glamour actually emits — headings, bullets, wrap columns — not a fake wrap.
//
// Height is derived from Body: Lines runs the same path Render does, so a
// caller that asks how tall a body will be gets the number the body actually is.
package markdown

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/muesli/termenv"

	"omakiten/internal/tui/components/screenkit"
)

// CacheCapacity bounds the render cache so a session that scrolls through long
// entity lists does not grow the map unboundedly. The unit is rendered
// (body, width) pairs.
const CacheCapacity = 64

// Renderer renders markdown bodies with an ansi.StyleConfig derived from Tokens.
// The (body hash, width) cache is bounded by CacheCapacity with LRU eviction;
// per-width *glamour.TermRenderer instances are reused across calls. Both
// caches clear on Reload.
type Renderer struct {
	tokens screenkit.MarkdownTokens
	style  ansi.StyleConfig

	mu        sync.Mutex
	cache     map[cacheKey]*list.Element
	order     *list.List
	renderers map[int]*glamour.TermRenderer
}

type cacheKey struct {
	bodyHash string
	width    int
}

type cacheEntry struct {
	key    cacheKey
	output string
}

// New builds a Renderer for the given tokens. The caller owns the instance
// and calls Reload when the tokens rotate.
func New(t screenkit.MarkdownTokens) *Renderer {
	return &Renderer{
		tokens:    t,
		style:     buildStyle(t),
		cache:     map[cacheKey]*list.Element{},
		order:     list.New(),
		renderers: map[int]*glamour.TermRenderer{},
	}
}

// Body honours the session-only rendered toggle every detail surface shares:
// when rendered is false the body is returned raw with the trailing newline
// stripped; when true, glamour wraps to width. Empty input short-circuits.
func Body(r *Renderer, body string, width int, rendered bool) string {
	raw := strings.TrimRight(sanitizeSource(body), "\n")
	if !rendered {
		return raw
	}
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	if r == nil {
		return raw
	}
	return r.Render(raw, width)
}

// Lines is the height [Body] would render at, without a second layout path.
// It runs Body and counts rows, so a caller that budgets a description before
// painting it cannot disagree with the paint.
func Lines(r *Renderer, body string, width int, rendered bool) int {
	out := Body(r, body, width, rendered)
	if out == "" {
		return 0
	}
	return strings.Count(out, "\n") + 1
}

// Render returns ANSI-styled output. Wrap is delegated to glamour at width.
// Empty input returns empty. Any glamour failure falls back to the original
// body — markdown is presentation, never correctness.
//
// The colour profile is pinned to TrueColor. It is NOT read from the
// environment: that is the whole point of extracting this package into the
// golden path. A fixture and the live app share one renderer.
func (r *Renderer) Render(body string, width int) string {
	body = sanitizeSource(body)
	if r == nil {
		return body
	}
	if strings.TrimSpace(body) == "" {
		return body
	}
	if width <= 0 {
		width = 80
	}

	key := cacheKey{bodyHash: hashBody(body), width: width}
	r.mu.Lock()
	defer r.mu.Unlock()
	if elem, ok := r.cache[key]; ok {
		r.order.MoveToFront(elem)
		return elem.Value.(cacheEntry).output
	}
	tr, ok := r.renderers[width]
	if !ok {
		var err error
		tr, err = glamour.NewTermRenderer(
			glamour.WithStyles(r.style),
			glamour.WithWordWrap(width),
			glamour.WithColorProfile(termenv.TrueColor),
		)
		if err != nil {
			return body
		}
		r.renderers[width] = tr
	}

	// tr.Render runs INSIDE the mutex: glamour's *TermRenderer threads
	// renderer state through goldmark's AST walker, and per-width reuse
	// turned that state into shared mutable structure across goroutines.
	out, err := tr.Render(body)
	if err != nil {
		return body
	}
	out = strings.Trim(out, "\n")

	elem := r.order.PushFront(cacheEntry{key: key, output: out})
	r.cache[key] = elem
	for r.order.Len() > CacheCapacity {
		oldest := r.order.Back()
		if oldest == nil {
			break
		}
		r.order.Remove(oldest)
		delete(r.cache, oldest.Value.(cacheEntry).key)
	}
	return out
}

// sanitizeSource is the terminal boundary for persisted Markdown. The
// multiline variant removes C0, C1, ESC and OSC controls without flattening LF,
// so raw mode and Glamour receive the same safe Markdown document.
func sanitizeSource(body string) string {
	if utf8.ValidString(body) && strings.IndexFunc(body, func(r rune) bool { return r != '\n' && unicode.IsControl(r) }) < 0 {
		return body
	}
	return screenkit.SanitizeMultiline(body)
}

// Reload rebuilds the StyleConfig from the new tokens and drops every cached
// entry. Called when the active theme rotates. Same-token Reload is a no-op
// so a paint path can call it every frame without dropping the LRU.
func (r *Renderer) Reload(t screenkit.MarkdownTokens) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tokens == t {
		return
	}
	r.tokens = t
	r.style = buildStyle(t)
	r.cache = map[cacheKey]*list.Element{}
	r.order = list.New()
	r.renderers = map[int]*glamour.TermRenderer{}
}

func buildStyle(t screenkit.MarkdownTokens) ansi.StyleConfig {
	primary := stringPtrIfSet(t.Primary)
	foreground := stringPtrIfSet(t.Foreground)
	border := stringPtrIfSet(t.Border)
	secondary := stringPtrIfSet(t.Secondary)
	bold := boolPtr(true)
	italic := boolPtr(true)
	underline := boolPtr(true)
	zero := uintPtr(0)
	one := uintPtr(1)
	two := uintPtr(2)

	heading := ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			BlockSuffix: "\n",
			Color:       primary,
			Bold:        bold,
		},
	}

	return ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: foreground},
			Margin:         zero,
		},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: secondary},
			Indent:         one,
			IndentToken:    stringPtr("│ "),
		},
		Paragraph: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: foreground},
		},
		List: ansi.StyleList{
			StyleBlock:  ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: foreground}},
			LevelIndent: 2,
		},
		Heading: heading,
		H1:      heading,
		H2:      heading,
		H3:      heading,
		H4:      heading,
		H5:      heading,
		H6:      heading,
		Strikethrough: ansi.StylePrimitive{
			CrossedOut: bold,
		},
		Emph: ansi.StylePrimitive{
			Italic: italic,
			Color:  foreground,
		},
		Strong: ansi.StylePrimitive{
			Bold:  bold,
			Color: foreground,
		},
		HorizontalRule: ansi.StylePrimitive{
			Color:  border,
			Format: "\n──────\n",
		},
		Item: ansi.StylePrimitive{
			BlockPrefix: "· ",
			Color:       border,
		},
		Enumeration: ansi.StylePrimitive{
			BlockPrefix: ". ",
			Color:       border,
		},
		Task: ansi.StyleTask{
			Ticked:   "[x] ",
			Unticked: "[ ] ",
		},
		Link: ansi.StylePrimitive{
			Color:     primary,
			Underline: underline,
		},
		LinkText: ansi.StylePrimitive{
			Color: primary,
		},
		ImageText: ansi.StylePrimitive{
			Color:  border,
			Format: imageFormat(t),
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: foreground,
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{Color: foreground},
				Margin:         two,
			},
		},
		Table: ansi.StyleTable{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{Color: foreground},
			},
		},
		DefinitionDescription: ansi.StylePrimitive{
			BlockPrefix: "\n· ",
		},
	}
}

func hashBody(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

func imageFormat(t screenkit.MarkdownTokens) string {
	if t.ImageFormat != "" {
		return t.ImageFormat
	}
	return tr(nil, "tui.markdown.image_fmt", "Image: {{.text}}")
}

func tr(text func(string) string, key, fallback string, args ...any) string {
	tmpl := fallback
	if text != nil {
		if got := text(key); got != "" && got != key {
			tmpl = got
		}
	}
	if len(args) == 0 {
		return tmpl
	}
	// args unused for image format today; keep signature aligned with other tr helpers
	_ = args
	return tmpl
}

func stringPtr(s string) *string { return &s }

func stringPtrIfSet(s string) *string {
	if s == "" {
		return nil
	}
	return stringPtr(s)
}

func boolPtr(b bool) *bool { return &b }

func uintPtr(u uint) *uint { return &u }
