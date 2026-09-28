// Package screenfixture builds the host-owned inputs a screenhost.Screen needs
// — an English-resolved frame at a pinned geometry — without importing testing.
//
// Goldens and assertions live in screentest, which wraps these builders with
// testing.TB. A main binary (cmd/okt-gallery) can import this package to paint
// the same fixtures the goldens already use; that is why the builders do not
// take a testing.TB and do not depend on the testing package.
package screenfixture

import (
	"fmt"
	"regexp"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/screenhost"
)

// DefaultChromeRows is the chrome budget a full Stats route occupies in the
// real host: a four-row screen header (blank, breadcrumb, nav strip, nav rule)
// plus the two-row sub strip, and no status badge.
const DefaultChromeRows = 7

// Project is the pinned project context every screen fixture runs against.
func Project() domain.ProjectContext {
	return domain.ProjectContext{ID: 1, Name: "Omakiten", Slug: "omakiten"}
}

// Options tunes a fixture frame. The zero value is a 120x40 terminal with the
// default chrome budget and a wired English catalog.
type Options struct {
	Width      int
	Height     int
	ChromeRows int
	Status     string
	Focused    bool
	// NoCatalog builds a frame with no catalog resolver, so labels degrade to
	// their keys. Used to prove a screen never panics on an unwired host.
	NoCatalog bool
}

// Frame builds a fixture frame for a screen under test or under the gallery.
func Frame(options Options) (screenhost.Frame, error) {
	if options.Width == 0 {
		options.Width = 120
	}
	if options.Height == 0 {
		options.Height = 40
	}
	if options.ChromeRows == 0 {
		options.ChromeRows = DefaultChromeRows
	}
	frameOptions := screenhost.FrameOptions{
		Width:       options.Width,
		Height:      options.Height,
		ProjectID:   Project().ID,
		ProjectSlug: Project().Slug,
		Status:      options.Status,
		Focused:     options.Focused,
		ChromeRows:  options.ChromeRows,
		Styles:      Styles(),
		Markdown:    MarkdownTokens(),
	}
	if !options.NoCatalog {
		catalog, err := Catalog()
		if err != nil {
			return screenhost.Frame{}, err
		}
		frameOptions.Text = catalog.Get
	}
	return screenhost.NewFrame(frameOptions), nil
}

// FrameAt is the common case: a frame at the given width and height.
func FrameAt(width, height int) (screenhost.Frame, error) {
	return Frame(Options{Width: width, Height: height})
}

// Fixture card and column geometry, mirroring the constants the root package
// resolves the real theme against (`columnWidth` and `cardBoxWidth` in
// internal/tui/state.go). They are restated rather than imported because
// internal/tui imports the harness packages; a drift here shows up as a fixture
// diff, which is exactly the signal these fixtures exist to carry.
const (
	fixtureColumnWidth  = 28
	fixtureCardBoxWidth = 26
	fixtureHintBoxWidth = 60
)

// Styles is the screen-facing theme projection used by fixtures.
//
// GEOMETRY MIRRORS THE REAL THEME. Every field carries the border, padding,
// width and alignment that `(styles).screenStyles()` in internal/tui projects
// from the shipped theme, so a change to card chrome — a border set swapped, a
// padding widened, a card box narrowed — moves fixture bytes. A field left
// zero-valued is invisible to every fixture in the tree: that is exactly how
// the card blind spot arose, and every screen that painted through one of those
// fields recorded a baseline no styling change could move. Never add a field
// here without its geometry.
//
// COLOUR IS ONLY REQUIRED TO BE DETERMINISTIC. Assertions strip ANSI, so the
// palette never reaches a fixture byte. It stays populated because a coloured
// style exercises the styled render path and the visible-width padding that
// depends on it; the tones below are a fixed test palette, not the shipped one.
func Styles() screenkit.Styles {
	color := func(hex string) lipgloss.Color { return lipgloss.Color(hex) }
	fg := func(hex string) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(color(hex))
	}
	// Role tones, named after the theme tokens internal/tui resolves.
	var (
		border     = color("#494D64")
		foreground = color("#CAD3F5")
		primary    = color("#8AADF4")
		secondary  = color("#C6A0F6")
		success    = color("#A6DA95")
		warning    = color("#EED49F")
		errorTone  = color("#ED8796")
		badgeFg    = color("#24273A")
	)
	// box is the bordered, padded chrome shared by panels, cards, inputs and
	// the hint box — the shape a border or padding change has to move.
	box := func(tone lipgloss.Color, horizontalPadding int) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(tone).Padding(0, horizontalPadding)
	}
	badge := func(background lipgloss.Color) lipgloss.Style {
		return lipgloss.NewStyle().Background(background).Foreground(badgeFg).Padding(0, 1).Bold(true)
	}
	return screenkit.Styles{
		Panel:            box(border, 2),
		Border:           lipgloss.NewStyle().Foreground(border),
		Separator:        lipgloss.NewStyle().Foreground(border),
		Marker:           lipgloss.NewStyle().Foreground(primary).Bold(true),
		Info:             lipgloss.NewStyle().Foreground(secondary),
		Hint:             lipgloss.NewStyle().Foreground(border),
		HintAccent:       lipgloss.NewStyle().Foreground(primary),
		HintBox:          box(border, 2).Width(fixtureHintBoxWidth),
		Empty:            lipgloss.NewStyle().Foreground(border).Width(fixtureColumnWidth).Align(lipgloss.Center),
		Error:            lipgloss.NewStyle().Foreground(errorTone),
		Warning:          lipgloss.NewStyle().Foreground(warning),
		Success:          lipgloss.NewStyle().Foreground(success),
		BadgeInfo:        badge(secondary),
		BadgeLow:         badge(secondary),
		BadgeNormal:      badge(success),
		BadgeBlocker:     badge(warning),
		Card:             box(border, 1).Width(fixtureCardBoxWidth),
		CardSelected:     box(primary, 1).Bold(true).Width(fixtureCardBoxWidth),
		CardArchived:     box(border, 1).Foreground(border).Strikethrough(true).Width(fixtureCardBoxWidth),
		CommentCard:      box(border, 1),
		SystemEventCard:  box(border, 1).Foreground(secondary),
		Input:            box(border, 2),
		FormMultiline:    box(border, 2),
		Cursor:           lipgloss.NewStyle().Foreground(primary),
		Nav:              lipgloss.NewStyle().Foreground(secondary),
		ActiveNav:        lipgloss.NewStyle().Foreground(primary).Bold(true),
		CategoryTask:     fg("#8BD5CA"),
		CategoryComment:  fg("#F5BDE6"),
		CategoryPlan:     fg("#A6DA95"),
		CategoryAudit:    fg("#B8C0E0"),
		CategoryGuard:    fg("#EED49F"),
		CategoryTrick:    fg("#F0C6C6"),
		CategoryToolCall: fg("#7DC4E4"),
	}
}

// MarkdownTokens is the markdown colour set fixtures paint with. The hexes
// match Styles() so a body rendered through components/markdown and a body
// painted through the lipgloss theme agree on palette. TrueColor is forced
// inside the renderer, so these tokens — not the ambient TERM — decide the
// ANSI sequences that ansi.Strip later removes from the golden bytes.
func MarkdownTokens() screenkit.MarkdownTokens {
	return screenkit.MarkdownTokens{
		ThemeKey:   "screentest",
		Foreground: "#CAD3F5",
		Border:     "#494D64",
		Primary:    "#8AADF4",
		Secondary:  "#C6A0F6",
	}
}

// Key builds a key message for a keystroke spelled the way
// tea.KeyMsg.String() spells it (e.g. "down", "pgup", "ctrl+d", "G").
func Key(spelling string) tea.KeyMsg {
	switch spelling {
	case "up", "down", "left", "right", "home", "end", "pgup", "pgdown":
		return tea.KeyMsg{Type: keyTypes[spelling]}
	case "ctrl+u":
		return tea.KeyMsg{Type: tea.KeyCtrlU}
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(spelling)}
}

var keyTypes = map[string]tea.KeyType{
	"up":     tea.KeyUp,
	"down":   tea.KeyDown,
	"left":   tea.KeyLeft,
	"right":  tea.KeyRight,
	"home":   tea.KeyHome,
	"end":    tea.KeyEnd,
	"pgup":   tea.KeyPgUp,
	"pgdown": tea.KeyPgDown,
}

var ansiPattern = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

// StripANSI removes every CSI/SGR escape sequence so assertions can match
// plain text across terminal colour profiles.
func StripANSI(s string) string { return ansiPattern.ReplaceAllString(s, "") }

var (
	catalogOnce sync.Once
	catalog     *config.Catalog
	catalogErr  error
)

// Catalog returns a singleton Catalog backed by the bundled English pack, so
// screen assertions and gallery paint read as English literals rather than
// catalog keys.
func Catalog() (*config.Catalog, error) {
	catalogOnce.Do(func() {
		en, err := config.LoadBundledLanguage("en")
		if err != nil {
			catalogErr = err
			return
		}
		catalog = config.NewCatalog(&en, &en)
	})
	if catalogErr != nil {
		return nil, fmt.Errorf("load bundled en catalog: %w", catalogErr)
	}
	return catalog, nil
}
