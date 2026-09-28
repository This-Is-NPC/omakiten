package project

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/domain"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

func testPayload() Payload { return fixtureBasePayload() }

func TestScreenLifecycleOwnsFocusActivityAndResize(t *testing.T) {
	frame := screentest.FrameAt(t, 120, 40)
	screen := New().Apply(Result{Generation: 0, Payload: testPayload()})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	if screen.Focus() != FocusForm || screen.ActivityCursor() != -1 || screen.BodyScroll() != 0 {
		t.Fatalf("enter state = focus %v cursor %d scroll %d", screen.Focus(), screen.ActivityCursor(), screen.BodyScroll())
	}

	screen = screen.Update(frame, screentest.Key("tab")).Screen.(Screen)
	screen = screen.Update(frame, screentest.Key("tab")).Screen.(Screen)
	if screen.Focus() != FocusActivity || screen.ActivityCursor() != 0 {
		t.Fatalf("activity focus = %v cursor %d", screen.Focus(), screen.ActivityCursor())
	}
	screen = screen.Update(frame, screentest.Key("j")).Screen.(Screen)
	if screen.ActivityCursor() != 1 {
		t.Fatalf("activity cursor = %d, want 1", screen.ActivityCursor())
	}

	tiny := screentest.FrameAt(t, 42, 12)
	screen = screen.Lifecycle(tiny, screenhost.LifecycleResize).Screen.(Screen)
	if screen.ActivityCursor() != 1 || screen.BodyScroll() < 0 {
		t.Fatalf("resize lost selection: cursor=%d scroll=%d", screen.ActivityCursor(), screen.BodyScroll())
	}
}

func TestScreenOutcomes(t *testing.T) {
	frame := screentest.FrameAt(t, 120, 40)
	screen := New().Apply(Result{Payload: testPayload()})
	if !screen.OwnsKey(screentest.Key("f")) {
		t.Fatal("project did not claim its form key")
	}

	if out := screen.Update(frame, screentest.Key("f")); out.Action.Kind != screenhost.ActionOpenProjectForm {
		t.Fatalf("f action = %v", out.Action.Kind)
	}
	if out := screen.Update(frame, screentest.Key("esc")); out.Action.Kind != screenhost.ActionBack {
		t.Fatalf("esc action = %v", out.Action.Kind)
	}
	out := screen.Update(frame, screentest.Key("r"))
	if out.Action.Kind != screenhost.ActionReload || out.Action.Generation != 1 || !out.Screen.(Screen).IsLoading() {
		t.Fatalf("reload outcome = %+v", out)
	}

	screen = screen.Update(frame, screentest.Key("tab")).Screen.(Screen)
	screen = screen.Update(frame, screentest.Key("tab")).Screen.(Screen)
	out = screen.Update(frame, screentest.Key("enter"))
	if out.Action.Kind != screenhost.ActionOpenProjectComment || out.Action.CommentID != 21 {
		t.Fatalf("comment outcome = %+v", out.Action)
	}
	screen = screen.Update(frame, screentest.Key("j")).Screen.(Screen)
	out = screen.Update(frame, screentest.Key("enter"))
	if out.Action.Kind != screenhost.ActionOpenTask || out.Action.TaskID != 12 {
		t.Fatalf("task outcome = %+v", out.Action)
	}
}

func TestScreenDropsStaleResultsAndAppliesCurrentError(t *testing.T) {
	screen := New().Loading(3)
	stale := screen.Apply(Result{Generation: 2, Payload: testPayload()})
	if len(stale.Activity()) != 0 || !stale.IsLoading() {
		t.Fatalf("stale result applied: %+v", stale)
	}
	wantErr := errors.New("project load failed")
	fresh := stale.Apply(Result{Generation: 3, Err: wantErr})
	if fresh.Err() != wantErr || fresh.IsLoading() {
		t.Fatalf("fresh error = %v loading=%v", fresh.Err(), fresh.IsLoading())
	}
}

func TestScreenViewAndFooter(t *testing.T) {
	frame := screentest.FrameAt(t, 160, 45)
	screen := New().Apply(Result{Payload: testPayload()})
	view := screentest.StripANSI(screen.View(frame))
	for _, want := range []string{"DEMO", "/tmp/demo", "BACKLOG", "first project note"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	// The description sits at the foot of the meta panel, and the meta zone now
	// holds a window of its own rather than being painted at whatever height it
	// wants inside one document. So it is REACHABLE from its zone rather than
	// unconditionally on the first frame — which is what a windowed zone means.
	if !projectZoneReaches(t, frame, screen, "pgdown", "ProjectDescriptionMarker") {
		t.Fatalf("the meta zone never reveals the description:\n%s", view)
	}
	if lipgloss.Width(view) > frame.Width() {
		t.Fatalf("view width = %d, terminal = %d", lipgloss.Width(view), frame.Width())
	}
	keys := map[string]bool{}
	for _, binding := range screen.Footer(frame) {
		keys[binding.Key] = true
	}
	for _, key := range []string{"tab", "f", "r", "esc"} {
		if !keys[key] {
			t.Fatalf("footer missing %q: %+v", key, screen.Footer(frame))
		}
	}
}

func TestFormReaderLifecycleOutcomeAndView(t *testing.T) {
	frame := screentest.FrameAt(t, 100, 30)
	reader := NewForm().Apply(testPayload())
	reader = reader.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(FormScreen)
	if reader.ID() != screenhost.ProjectForm || reader.Scroll() != 0 || !reader.BlocksHostInput() {
		t.Fatalf("reader initial state: id=%q scroll=%d blocks=%v", reader.ID(), reader.Scroll(), reader.BlocksHostInput())
	}
	view := screentest.StripANSI(reader.View(frame))
	if !strings.Contains(view, "ProjectDescriptionMarker") || !strings.Contains(view, "/tmp/demo") {
		t.Fatalf("reader view missing project content:\n%s", view)
	}
	if out := reader.Update(frame, screentest.Key("f")); out.Action.Kind != screenhost.ActionBack {
		t.Fatalf("reader f action = %v", out.Action.Kind)
	}
	if out := reader.Update(frame, screentest.Key("M")); out.Action.Kind != screenhost.ActionSetStatus || out.Screen.(FormScreen).MarkdownRendered() {
		t.Fatalf("reader markdown outcome = %+v", out)
	}
}

func TestFormReaderNavigationUsesGridOffset(t *testing.T) {
	frame := screentest.FrameAt(t, 100, 16)
	reader := NewForm().Apply(testPayload()).Lifecycle(frame, screenhost.LifecycleEnter).Screen.(FormScreen)
	for i := 0; i < 8; i++ {
		reader = reader.Update(frame, screentest.Key("j")).Screen.(FormScreen)
	}
	if reader.Scroll() == 0 {
		t.Fatal("form reader j did not move the screengrid offset")
	}
	reader = reader.Update(frame, screentest.Key("k")).Screen.(FormScreen)
	if reader.Scroll() < 0 {
		t.Fatalf("form reader grid offset = %d", reader.Scroll())
	}
}

// TestProjectBodyFitsTheHostRowBudget pins the row budget the WHOLE screen body
// is allowed to occupy. The host paints ChromeRows above the body, the body
// itself opens with a blank line and the footer costs two rows, so everything
// the screen draws has to fit in Height-ChromeRows-3 terminal rows.
//
// Its predecessor asserted this of the activity panel alone, which was the
// tighter half of a looser truth: the panel could sit exactly on the budget
// while the meta panel and dashboard stacked above it pushed the whole column
// to more than twice the terminal's height. Only the composed body can carry
// the invariant, because only the composed body is what the terminal shows.
func TestProjectBodyFitsTheHostRowBudget(t *testing.T) {
	for _, geometry := range []struct{ width, height int }{{80, 24}, {120, 40}, {200, 50}} {
		frame := screentest.FrameAt(t, geometry.width, geometry.height)
		payload := testPayload()
		payload.Activity = nil
		for i := 0; i < 200; i++ {
			payload.Activity = append(payload.Activity, domain.Event{ID: int64(i), EventType: domain.EventTypeComment, EntityType: domain.EventEntityProject, EntityID: 7, Body: "activity row"})
		}
		screen := New().Apply(Result{Payload: payload})

		// View opens with the body's leading blank line, so the newline count is
		// exactly the number of body rows the host will paint under the chrome.
		got := strings.Count(screentest.StripANSI(screen.View(frame)), "\n")
		budget := frame.Height() - frame.ChromeRows() - projectBodyChromeRows
		if got > budget {
			t.Errorf("%dx%d: body = %d rows, host body budget = %d", geometry.width, geometry.height, got, budget)
		}

		// The budget is a ceiling AND a floor: a body that shrank below it would
		// waste rows the content is entitled to.
		if got != budget {
			t.Errorf("%dx%d: body = %d rows, want exactly the %d-row budget", geometry.width, geometry.height, got, budget)
		}
	}
}

// TestProjectViewportRowsIsSingleSourced is RETIRED by the screenlayout
// migration (#2425). The screen no longer has a row budget to single-source:
// `viewportRows` is gone along with the document window it sized, and the only
// budget in play is screenlayout.BodyRows, which the arranger derives and pins
// against the kit at screenlayout.TestBodyRowsAgreesWithKitViewportRowsAboveTheSentinel.
// What this screen still owes is that the arranged body SPENDS that budget
// exactly, which TestProjectBodyFitsTheHostRowBudget above asserts in both
// directions.

// projectBodyChromeRows is the host accounting restated independently of the
// production helper so the row-budget assertions below are an oracle rather
// than a tautology: one leading blank line opened by every screen body, plus
// the newline and the indented keybinding row the host appends underneath it.
const projectBodyChromeRows = 3

// projectZoneReaches drives one key from the given state and reports whether
// the marker ever entered a rendered frame. Mirrors the cross-screen
// reachability driver in internal/tui: press until the frame stops changing or
// the marker shows up.
func projectZoneReaches(t *testing.T, frame screenhost.Frame, screen Screen, key, marker string) bool {
	t.Helper()
	previous := ""
	prevCursor, prevOffset := screen.ActivityCursor(), screen.zoneScroll(screen.focusedSection())
	for press := 0; press < 200; press++ {
		view := screentest.StripANSI(screen.View(frame))
		if strings.Contains(view, marker) {
			return true
		}
		cursor, offset := screen.ActivityCursor(), screen.zoneScroll(screen.focusedSection())
		// Selection-only paints differ only by border colour, which StripANSI
		// removes — so an unchanged stripped view is not stuck while the cursor
		// or zone offset is still moving.
		if press > 0 && view == previous && cursor == prevCursor && offset == prevOffset {
			return false
		}
		previous, prevCursor, prevOffset = view, cursor, offset
		screen = screen.Update(frame, screentest.Key(key)).Screen.(Screen)
	}
	return false
}

// TestProjectBodyFitsTheTerminalAndEveryZoneScrollsItsOwnTail is the
// regression test for the defect a user hit opening the project view with
// ctrl+p on an 80x24 terminal: the screen painted its meta panel, dashboard and
// activity feed as one unclipped column, so everything past the fold was drawn
// into rows the terminal does not have — and no key moved it.
//
// Two things are asserted together because either alone is satisfiable without
// fixing the defect. A screen can fit the budget by clipping and stranding its
// tail; a screen can scroll a zone nobody can see. So: the body the screen
// paints must fit the host row budget, AND every zone's own tail must be
// reachable from that zone, in both layout branches.
//
// # Why "its own" rather than "every zone's"
//
// It used to require the dashboard tail AND the feed tail from all three zones,
// which was the whole-screen semantics of the one shared document window #2415
// shipped. The Owner retired that reading (comment 123670): `tab` is a key this
// screen declares, so "reachable through the screen's own keys" has always
// included changing zones, and requiring one zone's rows from every other zone
// quietly makes a second scrollable zone illegal. The zones scroll
// independently again (comment 122985), so each one answers for its own rows —
// and nothing is relaxed: every zone still has to make every one of its rows
// reachable, at both geometries.
func TestProjectBodyFitsTheTerminalAndEveryZoneScrollsItsOwnTail(t *testing.T) {
	const activityTail = "ActivityTailMarker"

	payload := testPayload()
	payload.Description = ""
	for i := 1; i <= 40; i++ {
		payload.Description += fmt.Sprintf("description line %02d\n", i)
	}
	payload.Activity = nil
	for i := 0; i < 40; i++ {
		body := "activity row"
		if i == 39 {
			body = activityTail
		}
		payload.Activity = append(payload.Activity, domain.Event{
			ID: int64(i + 1), EventType: domain.EventTypeComment,
			EntityType: domain.EventEntityProject, EntityID: 7, Body: body,
		})
	}

	geometries := []struct {
		name          string
		width, height int
	}{{"stacked", 80, 24}, {"side-by-side", 120, 40}}
	// Each zone owns a marker that exists only in its own content: the meta
	// panel's capped description tail, the dashboard's plan-percent row, the
	// last card of the feed.
	zones := []struct {
		name   string
		prefix []string
		tail   string
	}{
		{"form", nil, "more lines"},
		{"dashboard", []string{"tab"}, "// PERCENT"},
		{"activity", []string{"tab", "tab"}, activityTail},
	}

	for _, geometry := range geometries {
		for _, zone := range zones {
			t.Run(geometry.name+"/"+zone.name, func(t *testing.T) {
				assertProjectBodyZone(t, payload, geometry.width, geometry.height, zone.name, zone.prefix, zone.tail)
			})
		}
	}
}

func assertProjectBodyZone(t *testing.T, payload Payload, width, height int, name string, prefix []string, tail string) {
	t.Helper()
	frame := screentest.FrameAt(t, width, height)
	budget := frame.Height() - frame.ChromeRows() - projectBodyChromeRows
	screen := New().Apply(Result{Payload: payload})
	for _, key := range prefix {
		screen = screen.Update(frame, screentest.Key(key)).Screen.(Screen)
	}
	first := screentest.StripANSI(screen.View(frame))
	if rows := strings.Count(first, "\n"); rows != budget {
		t.Fatalf("%dx%d: body paints %d rows into a %d-row budget", width, height, rows, budget)
	}
	if strings.Contains(first, tail) {
		t.Fatalf("%dx%d zone %q: the tail is already on the first frame; this geometry proves nothing about scrolling", width, height, name)
	}
	if projectZoneReaches(t, frame, screen, "pgdown", tail) || projectZoneReaches(t, frame, screen, "j", tail) {
		return
	}
	t.Fatalf("%dx%d zone %q: %q is never reachable through the screen's own motion keys", width, height, name, tail)
}

func TestScreenNavigationAndPresentationEdges(t *testing.T) {
	frame := screentest.FrameAt(t, 46, 14)
	screen := New().Apply(Result{Payload: testPayload()})
	assertProjectCapabilities(t, frame, screen)
	screen = assertProjectNarrowNavigation(t, frame, screen)
	if screen.Focus() != FocusForm {
		t.Fatalf("cycling off the activity zone landed on %v", screen.Focus())
	}
	if !projectZoneReaches(t, frame, screen, "pgup", "ProjectDescriptionMarker") && !projectZoneReaches(t, frame, screen, "pgdown", "ProjectDescriptionMarker") {
		t.Fatal("narrow view never reveals the description through the screen's own paging keys")
	}
}

func assertProjectCapabilities(t *testing.T, frame screenhost.Frame, screen Screen) {
	t.Helper()
	if screen.ID() != screenhost.Project || screen.Generation() != 0 || screen.Payload().Project.ID != 7 {
		t.Fatalf("screen identity/projection mismatch")
	}
	if len(screen.Help(frame)) == 0 || !screen.OwnsFooter() {
		t.Fatal("screen must own help and footer")
	}
	for _, key := range []string{"tab", "j", "k", "down", "up", "pgdown", "pgdn", "pgup", "g", "home", "G", "end", "n", "e", "c", "m", "A", "r", "esc", "f", "enter"} {
		if !screen.OwnsKey(screentest.Key(key)) {
			t.Fatalf("screen does not own %q", key)
		}
	}
	if screen.OwnsKey(screentest.Key("x")) {
		t.Fatal("screen unexpectedly owns x")
	}
	if out := screen.Update(frame, struct{}{}); out.Action.Kind != screenhost.ActionNone {
		t.Fatalf("non-key action = %v", out.Action.Kind)
	}
}

func assertProjectNarrowNavigation(t *testing.T, frame screenhost.Frame, screen Screen) Screen {
	t.Helper()
	screen = screen.Update(frame, screentest.Key("shift+tab")).Screen.(Screen)
	// Frame is 46×14: HostBox=4 rows, zone floors 3+3+3=9, so dashboard and
	// activity drop under the hard MinRows floor. shift+tab is tops, not a
	// zone key, so the screen leaves it; activity is unreachable — FocusForm, cursor -1.
	if screen.Focus() != FocusForm || screen.ActivityCursor() != -1 {
		t.Fatalf("unowned shift+tab focus = %v cursor=%d, want FocusForm/-1 (activity dropped at this geometry)", screen.Focus(), screen.ActivityCursor())
	}
	for _, key := range []string{"end", "up", "down", "pgdown", "pgup", "home", "G", "g"} {
		screen = screen.Update(frame, screentest.Key(key)).Screen.(Screen)
	}
	// Activity is still dropped: scroll keys cannot select a card that has no
	// zone. Cursor stays -1; BodyScroll is the surviving meta zone's offset.
	if screen.ActivityCursor() != -1 || screen.BodyScroll() < 0 {
		t.Fatalf("navigation ended at cursor=%d scroll=%d, want -1 / non-negative (activity dropped)", screen.ActivityCursor(), screen.BodyScroll())
	}
	for _, key := range []string{"n", "e", "c", "m", "A"} {
		if out := screen.Update(frame, screentest.Key(key)); out.Action.Kind != screenhost.ActionNone {
			t.Fatalf("read-only key %q action = %v", key, out.Action.Kind)
		}
	}
	return screen.Update(frame, screentest.Key("tab")).Screen.(Screen)
}

func TestScreenEmptyLoadingErrorAndProjectionClamp(t *testing.T) {
	frame := screentest.FrameAt(t, 80, 24)
	loading := New().Loading(1)
	if got := screentest.StripANSI(loading.View(frame)); !strings.Contains(got, "Loading") {
		t.Fatalf("loading view = %q", got)
	}
	failed := loading.Apply(Result{Generation: 1, Err: errors.New("boom")})
	if got := screentest.StripANSI(failed.View(frame)); !strings.Contains(got, "boom") {
		t.Fatalf("error view = %q", got)
	}

	screen := New().Apply(Result{Payload: Payload{Project: domain.ProjectContext{ID: 7, Slug: "demo"}}})
	for _, key := range []string{"tab", "tab", "j", "k", "pgdown", "pgup"} {
		screen = screen.Update(frame, screentest.Key(key)).Screen.(Screen)
	}
	if screen.ActivityCursor() != -1 || screen.BodyScroll() < 0 {
		t.Fatalf("empty activity state = cursor=%d scroll=%d", screen.ActivityCursor(), screen.BodyScroll())
	}
	// An empty feed no longer pins the document at the top: the meta panel and
	// dashboard above it still overflow an 80x24 terminal, so both empty states
	// have to be reachable rather than merely rendered into rows that are not
	// on screen. Each is asked of the zone that owns it — the zones hold
	// independent scroll surfaces (comment 122985), so "reachable" means
	// reachable from the zone whose content it is.
	for _, want := range []struct {
		marker string
		zone   Focus
	}{{"No project description", FocusForm}, {"No project activity", FocusActivity}} {
		focused := screen
		for focused.Focus() != want.zone {
			focused = focused.Update(frame, screentest.Key("tab")).Screen.(Screen)
		}
		if !projectZoneReaches(t, frame, focused, "pgdown", want.marker) && !projectZoneReaches(t, frame, focused, "pgup", want.marker) {
			t.Fatalf("empty view never reveals %q through the screen's own paging keys", want.marker)
		}
	}
}

func TestFormReaderRemainingContract(t *testing.T) {
	frame := screentest.FrameAt(t, 80, 16)
	reader := NewForm().Apply(Payload{Project: domain.ProjectContext{ID: 7, Slug: "demo"}})
	if !reader.OwnsKey(screentest.Key("x")) || !reader.OwnsFooter() || len(reader.Footer(frame)) == 0 || len(reader.Help(frame)) == 0 {
		t.Fatal("reader contract capabilities missing")
	}
	if out := reader.Update(frame, struct{}{}); out.Action.Kind != screenhost.ActionNone {
		t.Fatalf("non-key action = %v", out.Action.Kind)
	}
	for _, key := range []string{"j", "pgdown", "g"} {
		reader = reader.Update(frame, screentest.Key(key)).Screen.(FormScreen)
	}
	reader = reader.Lifecycle(frame, screenhost.LifecycleResize).Screen.(FormScreen)
	reader = reader.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(FormScreen)
	if got := screentest.StripANSI(reader.View(frame)); !strings.Contains(got, "PROJECT · DEMO") {
		t.Fatalf("empty reader view:\n%s", got)
	}
	if out := reader.Update(frame, screentest.Key("esc")); out.Action.Kind != screenhost.ActionBack {
		t.Fatalf("reader esc action = %v", out.Action.Kind)
	}
}

// TestProjectZonesHoldIndependentScrollOffsets is the executable form of the
// obligation comment 122985 recorded against #2415: the interim fix collapsed
// all three zones onto ONE document offset, so scrolling to read the feed on a
// wide terminal dragged the project meta out of view.
//
// Per-section offsets are what the arranger exists to give back. Paging the
// zone that holds focus must move that zone and leave its siblings where they
// were, at both layout branches.
func TestProjectZonesHoldIndependentScrollOffsets(t *testing.T) {
	payload := testPayload()
	payload.Activity = nil
	for i := 0; i < 60; i++ {
		payload.Activity = append(payload.Activity, domain.Event{
			ID: int64(i + 1), EventType: domain.EventTypeComment,
			EntityType: domain.EventEntityProject, EntityID: 7, Body: "activity row",
		})
	}
	payload.Dashboard.Buckets = nil
	for i := 0; i < 30; i++ {
		payload.Dashboard.Buckets = append(payload.Dashboard.Buckets, BucketCount{Name: "bucket", Count: i})
	}

	for _, geometry := range []struct {
		name          string
		width, height int
	}{
		{"stacked", 80, 24},
		{"side-by-side", 120, 40},
	} {
		t.Run(geometry.name, func(t *testing.T) {
			assertProjectZoneOffsets(t, payload, geometry.width, geometry.height)
		})
	}
}

func assertProjectZoneOffsets(t *testing.T, payload Payload, width, height int) {
	t.Helper()
	frame := screentest.FrameAt(t, width, height)
	screen := New().Apply(Result{Payload: payload})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	_ = screen.View(frame)
	for _, key := range []string{"tab", "tab"} {
		screen = screen.Update(frame, screentest.Key(key)).Screen.(Screen)
	}
	if screen.Focus() != FocusActivity {
		t.Fatalf("two tabs landed on %v, want FocusActivity", screen.Focus())
	}
	moved := false
	for press := 0; press < 40; press++ {
		screen = screen.Update(frame, screentest.Key("pgdown")).Screen.(Screen)
		_ = screen.View(frame)
		if screen.zoneScroll(sectionActivity) > 0 {
			moved = true
			break
		}
	}
	if !moved {
		t.Fatal("paging the focused feed moved no offset; the measurement is vacuous")
	}
	if meta, dash := screen.zoneScroll(sectionMeta), screen.zoneScroll(sectionDashboard); meta != 0 || dash != 0 {
		t.Errorf("paging the feed also moved meta to %d and dashboard to %d; the zones share a scroll surface", meta, dash)
	}
}
