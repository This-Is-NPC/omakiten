// Package screentest is the assertion half of the screen fixture harness.
// Scenario builders — Frame, Styles, Catalog, Geometries — live in
// screenfixture so a main binary can import them without pulling testing.
// This package re-exports those builders with testing.TB where a test needs
// Helper/Fatal, and owns Record, goldens, and the row-budget assertions.
package screentest

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/testutil"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screenhost"
)

// DefaultChromeRows is the chrome budget a full Stats route occupies in the
// real host. See screenfixture.DefaultChromeRows.
const DefaultChromeRows = screenfixture.DefaultChromeRows

// Project is the pinned project context every screen fixture runs against.
func Project() domain.ProjectContext { return screenfixture.Project() }

// Options tunes a fixture frame. See screenfixture.Options.
type Options = screenfixture.Options

// Frame builds a fixture frame for a screen under test.
func Frame(tb testing.TB, options Options) screenhost.Frame {
	tb.Helper()
	frame, err := screenfixture.Frame(options)
	if err != nil {
		tb.Fatal(err)
	}
	return frame
}

// FrameAt is the common case: a frame at the given width and height.
func FrameAt(tb testing.TB, width, height int) screenhost.Frame {
	tb.Helper()
	return Frame(tb, Options{Width: width, Height: height})
}

// Styles is the screen-facing theme projection used by fixtures.
func Styles() screenkit.Styles { return screenfixture.Styles() }

// MarkdownTokens is the markdown colour set fixtures paint with.
func MarkdownTokens() screenkit.MarkdownTokens { return screenfixture.MarkdownTokens() }

// Key builds a key message for a keystroke spelled the way
// tea.KeyMsg.String() spells it (e.g. "down", "pgup", "ctrl+d", "G").
func Key(spelling string) tea.KeyMsg { return screenfixture.Key(spelling) }

// StripANSI removes every CSI/SGR escape sequence so assertions can match
// plain text across terminal colour profiles.
func StripANSI(s string) string { return screenfixture.StripANSI(s) }

// Catalog returns a singleton Catalog backed by the bundled English pack, so
// screen assertions read as English literals rather than catalog keys.
func Catalog(tb testing.TB) *config.Catalog {
	tb.Helper()
	catalog, err := screenfixture.Catalog()
	if err != nil {
		tb.Fatal(err)
	}
	return catalog
}

// HydrateEventRegistry populates the domain event registry from the embedded
// omakase kit. Screen packages whose renderers call domain.EventCategoryOf or
// domain.SummarizeEvent call it from TestMain.
func HydrateEventRegistry() error { return testutil.HydrateDomainEventRegistry() }

// Geometry is one terminal a baseline is recorded at.
type Geometry = screenfixture.Geometry

// Geometries are the three terminals every screen baseline is recorded at.
var Geometries = screenfixture.Geometries
