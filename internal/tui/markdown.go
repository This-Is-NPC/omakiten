package tui

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/muesli/termenv"

	"omakiten/internal/config"
)

// markdownCacheCapacity bounds the render cache so a session that
// scrolls through long entity lists does not grow the map unboundedly.
// The unit is "rendered (body, width) pairs"; 64 is a generous fit
// for any single view's working set and still tiny on memory.
const markdownCacheCapacity = 64

// markdownTokens is the slim subset of theme colors the markdown renderer
// needs. Pulled from the resolved theme via tokensFromTheme so the renderer
// never re-reads the YAML — and keeping it as a flat struct (instead of the
// raw map) makes the cache key trivially hashable.
type markdownTokens struct {
	themeKey   string
	foreground string
	border     string
	primary    string
	secondary  string
}

// tokensFromTheme extracts the four tokens the markdown renderer consumes
// from the active theme. Empty strings are kept so the StyleConfig builder
// can fall through to glamour's default (no Color set).
func tokensFromTheme(theme config.Theme) markdownTokens {
	pick := func(key string) string { return theme.Colors[key] }
	return markdownTokens{
		themeKey:   theme.Key,
		foreground: pick("foreground"),
		border:     pick("border"),
		primary:    pick("primary"),
		secondary:  pick("secondary"),
	}
}

// markdownRenderer renders markdown bodies with an ansi.StyleConfig derived
// from the active theme tokens. The (body hash, width) cache is bounded by
// markdownCacheCapacity with LRU eviction so long sessions cannot leak
// memory through scroll history; the per-width *glamour.TermRenderer
// instances are reused across calls instead of being rebuilt on every
// cache miss. Both caches are cleared on theme change.
type markdownRenderer struct {
	tokens markdownTokens
	style  ansi.StyleConfig

	mu        sync.Mutex
	cache     map[markdownCacheKey]*list.Element
	order     *list.List // newest-first; Back() is the LRU eviction victim
	renderers map[int]*glamour.TermRenderer
}

type markdownCacheKey struct {
	bodyHash string
	width    int
}

type markdownCacheEntry struct {
	key    markdownCacheKey
	output string
}

func newMarkdownRenderer(t markdownTokens) *markdownRenderer {
	return &markdownRenderer{
		tokens:    t,
		style:     buildMarkdownStyle(t),
		cache:     map[markdownCacheKey]*list.Element{},
		order:     list.New(),
		renderers: map[int]*glamour.TermRenderer{},
	}
}

// Render returns ANSI-styled output suitable for a gridtable Span. Wrap is
// delegated to glamour at `width`. Empty input returns the empty string
// untouched. Any glamour failure falls back to the original body so the
// caller still gets readable text — markdown is a presentation concern,
// never a correctness one.
func (r *markdownRenderer) Render(body string, width int) string {
	if r == nil {
		return body
	}
	if strings.TrimSpace(body) == "" {
		return body
	}
	if width <= 0 {
		width = 80
	}

	key := markdownCacheKey{bodyHash: hashBody(body), width: width}
	r.mu.Lock()
	defer r.mu.Unlock()
	if elem, ok := r.cache[key]; ok {
		r.order.MoveToFront(elem)
		return elem.Value.(markdownCacheEntry).output
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
	// renderer state through goldmark's AST walker (BlockStack +
	// per-element ANSIRenderer fields), and the W3 #215 per-width reuse
	// turned that state into shared mutable structure across goroutines.
	// Cache hits short-circuit above, so the only contention is on
	// distinct miss paths — acceptable since miss-path Render dominates
	// the per-call cost regardless.
	out, err := tr.Render(body)
	if err != nil {
		return body
	}
	out = strings.Trim(out, "\n")

	elem := r.order.PushFront(markdownCacheEntry{key: key, output: out})
	r.cache[key] = elem
	for r.order.Len() > markdownCacheCapacity {
		oldest := r.order.Back()
		if oldest == nil {
			break
		}
		r.order.Remove(oldest)
		delete(r.cache, oldest.Value.(markdownCacheEntry).key)
	}
	return out
}

// reloadTheme rebuilds the StyleConfig from the new theme tokens and
// drops every cached entry (output + per-width renderers). Called by
// the runtime when the active theme rotates so the next Render call
// emits styles from the freshly-loaded palette.
func (r *markdownRenderer) reloadTheme(t markdownTokens) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tokens = t
	r.style = buildMarkdownStyle(t)
	r.cache = map[markdownCacheKey]*list.Element{}
	r.order = list.New()
	r.renderers = map[int]*glamour.TermRenderer{}
}

// buildMarkdownStyle maps the four theme tokens onto glamour's StyleConfig.
// Aligned with the dev-editorial design language: primary headings, plain
// foreground code (no chroma background), `·` bullets, and a `─` rule. The
// kicker carries section labels, so headings stay un-prefixed and rely on
// color/weight alone.
func buildMarkdownStyle(t markdownTokens) ansi.StyleConfig {
	primary := stringPtrIfSet(t.primary)
	foreground := stringPtrIfSet(t.foreground)
	border := stringPtrIfSet(t.border)
	secondary := stringPtrIfSet(t.secondary)
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
			Format: "Image: {{.text}}",
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

// toggleMarkdownRendered flips the session-only render mode and surfaces a
// status badge so the user gets confirmation that the keystroke landed.
// Called from every detail view that displays a body (task, comment,
// entity) — single helper keeps the status string consistent.
func (m *Model) toggleMarkdownRendered() {
	m.markdownRendered = !m.markdownRendered
	if m.markdownRendered {
		m.status = m.t("tui.status.markdown_rendered")
	} else {
		m.status = m.t("tui.status.markdown_raw")
	}
}

// renderBodyMarkdown is the surface helper called by every detail-panel
// renderer that displays a markdown body (task description, comment body,
// entity body). It honors the session-only `markdownRendered` toggle —
// when off, the body is returned raw with the trailing newline stripped
// so the caller's gridtable Span layout stays identical to the pre-toggle
// behavior. When on, glamour wraps to `width` and the kicker carries the
// section label so headings render in primary without prefix duplication.
func (m Model) renderBodyMarkdown(body string, width int) string {
	raw := strings.TrimRight(body, "\n")
	if !m.markdownRendered {
		return raw
	}
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	return m.markdown.Render(raw, width)
}

// hashBody is a short fingerprint suitable for an in-memory cache key. The
// 8-byte prefix of SHA-256 keeps the key compact (16 hex chars) while
// collision risk is irrelevant inside a single TUI session.
func hashBody(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
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
