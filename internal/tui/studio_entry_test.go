package tui

import (
	"path/filepath"
	"testing"

	"omakiten/internal/testfixtures/bundleeditor"
	"omakiten/internal/tui/screenhost"
)

// TestNavigatingIntoStudioOpensTheDraftBeforeTheFirstView is the host-side
// guard for the Studio draft hoist.
//
// The draft used to be built inside the render path, so the very first View of
// a Studio route happened to have one. Now it is opened at screen ENTRY, and
// Studio is a base route rather than a modal one — the host only dispatches
// LifecycleEnter to base routes that declare NavigationResetter. If that
// declaration is ever dropped, the outcome carrying the draft stops being
// stored, and every Studio render falls back to the snapshot bundle: no edit
// warnings, no blocked reason, a quietly different screen. This test fails the
// moment that wiring goes.
func TestNavigatingIntoStudioOpensTheDraftBeforeTheFirstView(t *testing.T) {
	store := &studioDraftStore{bundle: studioDraftBundle()}
	editor := bundleeditor.New(store, filepath.Join(t.TempDir(), "omakiten.yaml"))

	m := Model{
		styles:     newStyles(tuiTestTheme()),
		width:      120,
		height:     40,
		repos:      Repositories{Editor: editor},
		navigation: screenhost.StudioWorkflow,
	}
	if m.studioScreen.StudioDraftOpen() {
		t.Fatal("the model starts with a Studio draft already open; the property is vacuous")
	}

	// Exactly what the host runs when navigation lands on a new route.
	m.refreshAfterViewChangeCmd("")

	if !m.studioScreen.StudioDraftOpen() {
		t.Fatal("navigating into Studio stored no draft; the first View will render from the snapshot instead of the bundle")
	}
	if _, ok := m.activeHostedScreen(); !ok {
		t.Fatal("no hosted screen for the Studio route")
	}
	if got := m.boundStudioScreen(screenhost.StudioWorkflow).ID(); got != screenhost.StudioWorkflow {
		t.Fatalf("bound Studio screen id = %q, want %q", got, screenhost.StudioWorkflow)
	}
}
