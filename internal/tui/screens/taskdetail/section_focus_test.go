package taskdetail

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

func TestFFullscreensTheFocusedSection(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 120, 40)
	payload := stackedPathPayload()

	cases := []struct {
		name  string
		setup []string
		want  screenlayout.ID
	}{
		{"details", nil, sectionDetails},
		{"activity", []string{"tab"}, sectionActivity},
		{"subtasks", []string{"tab", "tab"}, sectionSubtasks},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertFFullscreensSection(t, frame, payload, tc.setup, tc.want)
		})
	}
}

// assertFFullscreensSection drives the setup keys, checks focus parked on
// the expected section, then checks f fullscreens exactly that section and
// paints only its leaf.
func assertFFullscreensSection(t *testing.T, frame screenhost.Frame, payload Payload, setup []string, want screenlayout.ID) {
	t.Helper()
	screen := New().Open(payload, frame)
	for _, k := range setup {
		screen = taskdetailDrive(t, frame, screen, k)
	}
	if taskdetailFocusID(screen) != want {
		t.Fatalf("setup parked on %q, want %q", taskdetailFocusID(screen), want)
	}
	out := screen.Update(frame, screentest.Key("f"))
	if out.Action.Kind != screenhost.ActionNone {
		t.Fatalf("f emitted %v; the focused section should fullscreen in place", out.Action.Kind)
	}
	screen = out.Screen.(Screen)
	if !screen.grid.Fullscreen() {
		t.Fatal("f did not fullscreen the focused section")
	}
	painted := taskdetailPaintedLeaves(taskdetailGrid(frame, screen))
	if len(painted) != 1 || painted[0] != want {
		t.Fatalf("fullscreen painted %v, want only %q", painted, want)
	}
}

func TestTabOnAShortStackedBodyFullscreensTheFocusedSection(t *testing.T) {
	t.Parallel()
	payload := stackedPathPayload()
	frame := screentest.FrameAt(t, 80, 24)
	screen := New().Open(payload, frame)
	if screenlayout.HostBox(screen.bodyKit(frame)).Rows >= detailsMinRows+subtasksMinRows+feedMinRows {
		t.Fatalf("80x24 HostBox still fits every zone floor; this case is vacuous")
	}

	want := []screenlayout.ID{sectionDetails, sectionActivity, sectionSubtasks}
	for i, id := range want {
		if i > 0 {
			screen = taskdetailDrive(t, frame, screen, "tab")
		}
		if taskdetailFocusID(screen) != id {
			t.Fatalf("tab %d parked on %q, want %q", i, taskdetailFocusID(screen), id)
		}
		painted := taskdetailPaintedLeaves(taskdetailGrid(frame, screen))
		if len(painted) != 1 || painted[0] != id {
			t.Fatalf("focus %q painted %v, want that section filling the stacked body", painted, id)
		}
	}
}

// TestTabOnATallStackedBodyKeepsFittedZones is the sharing half of the
// stacked rule: a body whose zones can all have their EXPECTED size — the
// screengrid arranger's own question, not merely their MinRows floor —
// shares them, exactly as a side-by-side body does, and `tab` only moves the
// focus. Losing a sibling the terminal has genuine room for is the defect
// Studio shipped (`fc15000f`), and task detail is not exempt from it just
// because one of its zones happens to declare a ceiling.
func TestTabOnATallStackedBodyKeepsFittedZones(t *testing.T) {
	t.Parallel()
	payload := stackedPathPayload()
	width, _ := deriveTaskdetailStackedWidth(t, payload)
	frame := screentest.FrameAt(t, width, 60)
	screen := New().Open(payload, frame)
	origin := taskdetailPaintedLeaves(taskdetailGrid(frame, screen))
	if len(origin) < 2 {
		t.Fatalf("tall stacked body painted %v; need at least two fitted zones", origin)
	}
	start := taskdetailFocusID(screen)
	for i := 0; i < len(origin); i++ {
		screen = taskdetailDrive(t, frame, screen, "tab")
		got := taskdetailPaintedLeaves(taskdetailGrid(frame, screen))
		if !taskdetailSamePainted(mapIDs(origin), got) {
			t.Fatalf("after tab to %q painted %v, want fitted zones %v", taskdetailFocusID(screen), got, origin)
		}
		if taskdetailFocusID(screen) == start {
			break
		}
	}
}

// TestTabOnASqueezedStackedBodyCascadesOnDetailsAndFullscreensElsewhere pins
// the OTHER half: a body whose details zone cannot have its ColumnMaxRows
// ceiling stops sharing and answers per focus instead — taken from
// internal/tui/layout's stackedFormSplit. Details is the fixed zone, so its
// focus cascades with whichever siblings the box can still afford — not by a
// floor-sum guess, but by whether details itself can still have its declared
// ceiling once a sibling is kept — while sub-tasks and activity are flexible
// and take the whole body on their own focus rather than painting squeezed
// beside their siblings.
//
// 92x44 is a geometry [distributeRows]'s weighted row split actually drops
// activity at: details' declared ceiling (21) is reachable beside sub-tasks
// alone, but not beside both sub-tasks AND activity at once, because the
// surplus splits by WEIGHT among every zone still in the running rather than
// filling details' ceiling first. A naive "does the floor sum fit" arithmetic
// cannot see that; only asking the allocator can.
func TestTabOnASqueezedStackedBodyCascadesOnDetailsAndFullscreensElsewhere(t *testing.T) {
	t.Parallel()
	payload := stackedPathPayload()
	frame := screentest.FrameAt(t, 92, 44)
	screen := New().Open(payload, frame)
	origin := taskdetailPaintedLeaves(taskdetailGrid(frame, screen))
	if taskdetailFocusID(screen) != sectionDetails {
		t.Fatalf("opened focused on %q, want details", taskdetailFocusID(screen))
	}
	if len(origin) != 2 || origin[0] != sectionDetails || origin[1] != sectionSubtasks {
		t.Fatalf("details focus painted %v, want details cascading with sub-tasks and activity dropped", origin)
	}
	if p, ok := taskdetailGrid(frame, screen).Placement(sectionDetails); !ok || p.Box.Rows != screen.detailsCeiling() {
		t.Fatalf("details cascaded at %+v, want its full column ceiling of %d", p, screen.detailsCeiling())
	}

	screen = taskdetailDrive(t, frame, screen, "tab")
	if taskdetailFocusID(screen) != sectionActivity {
		t.Fatalf("first tab parked on %q, want activity", taskdetailFocusID(screen))
	}
	if got := taskdetailPaintedLeaves(taskdetailGrid(frame, screen)); len(got) != 1 || got[0] != sectionActivity {
		t.Fatalf("activity focus painted %v, want only activity filling the body", got)
	}

	screen = taskdetailDrive(t, frame, screen, "tab")
	if taskdetailFocusID(screen) != sectionSubtasks {
		t.Fatalf("second tab parked on %q, want sub-tasks", taskdetailFocusID(screen))
	}
	if got := taskdetailPaintedLeaves(taskdetailGrid(frame, screen)); len(got) != 1 || got[0] != sectionSubtasks {
		t.Fatalf("sub-tasks focus painted %v, want only sub-tasks filling the body", got)
	}

	screen = taskdetailDrive(t, frame, screen, "tab")
	if taskdetailFocusID(screen) != sectionDetails {
		t.Fatalf("third tab parked on %q, want back on details", taskdetailFocusID(screen))
	}
	if got := taskdetailPaintedLeaves(taskdetailGrid(frame, screen)); !taskdetailSamePainted(mapIDs(origin), got) {
		t.Fatalf("details focus painted %v, want the same cascade %v it opened with", got, origin)
	}
}

func TestEscDropsFullscreenBeforeClosingTaskDetail(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 120, 40)
	screen := New().Open(stackedPathPayload(), frame)
	screen = taskdetailDrive(t, frame, screen, "f")
	if !screen.grid.Fullscreen() || taskdetailFocusID(screen) != sectionDetails {
		t.Fatalf("setup: fullscreen=%v focus=%q", screen.grid.Fullscreen(), taskdetailFocusID(screen))
	}
	out := screen.Update(frame, tea.KeyMsg{Type: tea.KeyEsc})
	dropped := out.Screen.(Screen)
	if out.Action.Kind != screenhost.ActionNone {
		t.Fatalf("first esc closed the screen (%v) instead of dropping fullscreen", out.Action.Kind)
	}
	if dropped.grid.Fullscreen() {
		t.Fatal("first esc left fullscreen on")
	}
	out = dropped.Update(frame, tea.KeyMsg{Type: tea.KeyEsc})
	if out.Action.Kind != screenhost.ActionCloseTaskDetail {
		t.Fatalf("second esc action = %v, want close", out.Action.Kind)
	}
}

func mapIDs(ids []screenlayout.ID) map[screenlayout.ID]bool {
	out := make(map[screenlayout.ID]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

func TestDetailsKeepsTheDescriptionPreviewOnATallSideBySideBody(t *testing.T) {
	t.Parallel()
	payload := testPayload()
	payload.Task.Description = strings.Join([]string{
		"Human-in-the-loop: the agent asks the human via the TUI before bypassing a guard.",
		"Second paragraph of the preview.",
		"Third paragraph of the preview.",
	}, "\n")
	frame := screentest.FrameAt(t, 122, 51)
	screen := New().Open(payload, frame)
	res := taskdetailGrid(frame, screen)
	details, ok := res.Placement(sectionDetails)
	if !ok {
		t.Fatal("details missing")
	}
	subtasks, ok := res.Placement(sectionSubtasks)
	if !ok {
		t.Fatal("subtasks missing")
	}
	if details.Box.Rows <= subtasks.Box.Rows {
		t.Fatalf("details %d rows vs subtasks %d; details should take the description preview first", details.Box.Rows, subtasks.Box.Rows)
	}
	view := res.View
	if !strings.Contains(view, "DESCRIPTION") {
		t.Fatal("details did not paint the description kicker")
	}
	if !strings.Contains(view, "Human-in-the-loop") {
		t.Fatal("details dropped the description preview")
	}
	if strings.Contains(view, "▼") {
		t.Fatalf("sharing details windowed the description; the preview cap should fit:\n%s", view)
	}
}

func TestActivityFillsTheAllocatedColumn(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 122, 51)
	screen := New().Open(testPayload(), frame)
	res := taskdetailGrid(frame, screen)
	root, ok := res.Placement("taskdetail")
	if !ok {
		t.Fatal("root missing")
	}
	activity, ok := res.Placement(sectionActivity)
	if !ok {
		t.Fatal("activity missing")
	}
	if activity.Box.Rows != root.Box.Rows {
		t.Fatalf("activity height %d, body %d; the feed should fill the column", activity.Box.Rows, root.Box.Rows)
	}
}

func TestSectionTitleRulesJoinTheSideBorders(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 122, 51)
	screen := New().Open(testPayload(), frame)
	view := taskdetailGrid(frame, screen).View
	if !strings.Contains(view, "├") {
		t.Fatalf("framed sections lost connecting title rules:\n%s", view)
	}
}

func TestInnerLaneChromeKeepsTheTaskGridBorder(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 122, 51)
	screen := New().Open(testPayload(), frame)
	view := taskdetailGrid(frame, screen).View
	if !strings.Contains(view, "┌") {
		t.Fatal("sub-task lanes did not paint a box")
	}
	if strings.Contains(view, "\x1b[39m┌") {
		t.Fatalf("inner lane top lost the grid border color (wrap sanitized SGR)")
	}
}

func TestTabOrderIsDetailsActivitySubtasks(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 122, 51)
	screen := New().Open(testPayload(), frame)
	want := []screenlayout.ID{sectionDetails, sectionActivity, sectionSubtasks}
	for i, id := range want {
		if i > 0 {
			screen = taskdetailDrive(t, frame, screen, "tab")
		}
		if taskdetailFocusID(screen) != id {
			t.Fatalf("tab %d parked on %q, want %q", i, taskdetailFocusID(screen), id)
		}
	}
}

func TestFOnDetailsFillsTheBodyAndScrollsTheDescription(t *testing.T) {
	t.Parallel()
	payload := testPayload()
	payload.Task.Description = strings.Repeat("Description paragraph that must remain reachable by j after fullscreen.\n\n", 20)
	frame := screentest.FrameAt(t, 122, 51)
	screen := New().Open(payload, frame)
	screen = taskdetailDrive(t, frame, screen, "f")
	if !screen.grid.Fullscreen() {
		t.Fatal("f did not fullscreen details")
	}
	res := taskdetailGrid(frame, screen)
	root, ok := res.Placement("taskdetail")
	if !ok {
		t.Fatal("root missing")
	}
	details, ok := res.Placement(sectionDetails)
	if !ok {
		t.Fatal("details missing")
	}
	if details.Box.Rows != root.Box.Rows {
		t.Fatalf("fullscreen details height %d, body %d; leftover rows were not given to the task", details.Box.Rows, root.Box.Rows)
	}
	before := screen.grid.Layout().Offset(sectionDetails)
	screen = taskdetailDrive(t, frame, screen, "j")
	if screen.grid.Layout().Offset(sectionDetails) <= before {
		t.Fatalf("j on fullscreen details left offset %d; the description should scroll", before)
	}
}

func TestActivityCardsPaintAFocusFrame(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 122, 51)
	screen := New().Open(testPayload(), frame)
	screen = taskdetailDrive(t, frame, screen, "tab")
	if taskdetailFocusID(screen) != sectionActivity {
		t.Fatalf("tab parked on %q, want activity", taskdetailFocusID(screen))
	}
	view := taskdetailGrid(frame, screen).View
	if !strings.Contains(view, "┌") {
		t.Fatal("activity feed lost card boxes")
	}
	if strings.Count(view, "┌") < 3 {
		t.Fatalf("activity feed did not paint inner card boxes:\n%s", view)
	}
}

func TestFOnActivityUsesTheBodyWidth(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 122, 51)
	screen := New().Open(testPayload(), frame)
	screen = taskdetailDrive(t, frame, screen, "tab")
	screen = taskdetailDrive(t, frame, screen, "f")
	if !screen.grid.Fullscreen() || taskdetailFocusID(screen) != sectionActivity {
		t.Fatalf("setup: fullscreen=%v focus=%q", screen.grid.Fullscreen(), taskdetailFocusID(screen))
	}
	res := taskdetailGrid(frame, screen)
	root, ok := res.Placement("taskdetail")
	if !ok {
		t.Fatal("root missing")
	}
	activity, ok := res.Placement(sectionActivity)
	if !ok {
		t.Fatal("activity missing")
	}
	if activity.Box.Rows != root.Box.Rows {
		t.Fatalf("fullscreen activity height %d, body %d", activity.Box.Rows, root.Box.Rows)
	}
	if activity.Box.Width != root.Box.Width {
		t.Fatalf("fullscreen activity width %d, body %d; cards were still measured as a side column", activity.Box.Width, root.Box.Width)
	}
}

func TestDescriptionStaysCappedUntilFullscreen(t *testing.T) {
	t.Parallel()
	payload := testPayload()
	payload.Task.Description = strings.Join([]string{
		"Preview line one stays on the shared column.",
		"Preview line two stays on the shared column.",
		"Preview line three stays on the shared column.",
		"Preview line four stays on the shared column.",
		"Preview line five stays on the shared column.",
		"UNIQUEPREVIEWTAIL must not appear until f fullscreens the task section.",
		strings.Repeat("More description for the fullscreen reader.\n\n", 20),
	}, "\n\n")
	frame := screentest.FrameAt(t, 122, 51)
	screen := New().Open(payload, frame)
	view := taskdetailGrid(frame, screen).View
	if !strings.Contains(view, "Preview line one") {
		t.Fatal("shared column lost the description preview")
	}
	if strings.Contains(view, "UNIQUEPREVIEWTAIL") {
		t.Fatalf("description opened past the five-line cap without f:\n%s", view)
	}
	if !strings.Contains(view, "f to focus") && !strings.Contains(view, "more lines") {
		t.Fatalf("capped description lost the f hint:\n%s", view)
	}
	before := screen.grid.Layout().Offset(sectionDetails)
	screen = taskdetailDrive(t, frame, screen, "j")
	if screen.grid.Layout().Offset(sectionDetails) != before {
		t.Fatalf("j scrolled the shared description (offset %d → %d)", before, screen.grid.Layout().Offset(sectionDetails))
	}
	screen = taskdetailDrive(t, frame, screen, "f")
	if !screen.grid.Fullscreen() {
		t.Fatal("f did not fullscreen details")
	}
	view = taskdetailGrid(frame, screen).View
	if !strings.Contains(view, "UNIQUEPREVIEWTAIL") {
		t.Fatalf("f did not open the rest of the description:\n%s", view)
	}
	before = screen.grid.Layout().Offset(sectionDetails)
	screen = taskdetailDrive(t, frame, screen, "j")
	if screen.grid.Layout().Offset(sectionDetails) <= before {
		t.Fatalf("j on fullscreen details left offset %d", before)
	}
}

func TestActivityScrollHintSitsInsideTheColumn(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 122, 51)
	screen := New().Open(stackedPathPayload(), frame)
	view := taskdetailGrid(frame, screen).View
	line := hintLine(view, "▼")
	if line == "" {
		t.Fatalf("activity feed with 40 events did not announce items below:\n%s", view)
	}
	if !strings.Contains(line, "│") {
		t.Fatalf("activity below hint sat on the border instead of inside the column:\n%s\nfull:\n%s", line, view)
	}
}

func TestSubtaskLaneAnnouncesItemsBelow(t *testing.T) {
	t.Parallel()
	payload := testPayload()
	parent := int64(10)
	for i := 0; i < 20; i++ {
		payload.Tasks = append(payload.Tasks, domain.Task{
			ID: int64(100 + i), Title: "overflow child", BucketKey: "dev", ParentID: &parent,
		})
	}
	frame := screentest.FrameAt(t, 122, 51)
	screen := New().Open(payload, frame)
	view := taskdetailGrid(frame, screen).View
	line := hintLine(view, "▼")
	if line == "" {
		t.Fatalf("overflowing sub-task lane did not announce items below:\n%s", view)
	}
	if !strings.Contains(line, "│") {
		t.Fatalf("sub-task below hint was not inside the lane:\n%s\nfull:\n%s", line, view)
	}
}

func hintLine(view, mark string) string {
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, mark) {
			return line
		}
	}
	return ""
}

func TestSubtasksFillTheLeftColumnBesideActivity(t *testing.T) {
	t.Parallel()
	payload := testPayload()
	payload.Task.Title = "Document architecture-review improvement opportunities (2026-07-03)"
	payload.Task.Description = strings.Join([]string{
		"Description",
		"",
		"As a maintainer of omakiten, I want the improvement opportunities recorded as a ranked backlog.",
		strings.Repeat("More description for the five-line preview cap.\n\n", 20),
	}, "\n\n")
	parent := int64(10)
	for i := 0; i < 12; i++ {
		payload.Tasks = append(payload.Tasks, domain.Task{
			ID: int64(200 + i), Title: "overflow child", BucketKey: "done", ParentID: &parent,
		})
	}
	frame := screentest.FrameAt(t, 122, 51)
	view := taskdetailGrid(frame, New().Open(payload, frame)).View
	lastLeft, lastRight := -1, -1
	for i, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "└") {
			if idx := strings.Index(line, "└"); idx >= 0 && idx < len(line)/2 {
				lastLeft = i
			}
			if idx := strings.LastIndex(line, "└"); idx > len(line)/3 {
				lastRight = i
			}
		}
	}
	if lastLeft < 0 || lastRight < 0 {
		t.Fatalf("missing section bottoms:\n%s", view)
	}
	if lastLeft != lastRight {
		t.Fatalf("sub-tasks ended at line %d, activity at %d; leftover height was not given to the board:\n%s", lastLeft, lastRight, view)
	}
}

func TestNestedTaskKickerKeepsTheBreadcrumb(t *testing.T) {
	t.Parallel()
	payload := testPayload()
	payload.Task = payload.Tasks[1] // child one (#11), parent #10
	frame := screentest.FrameAt(t, 122, 51)
	view := taskdetailGrid(frame, New().Open(payload, frame)).View
	if !strings.Contains(view, "TASK") || !strings.Contains(view, "#11") {
		t.Fatalf("nested task lost the kicker:\n%s", view)
	}
	if !strings.Contains(view, "← #10") {
		t.Fatalf("entering a sub-task lost the parent breadcrumb:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "TASK") && strings.Contains(line, "← #10") {
			return
		}
	}
	t.Fatalf("breadcrumb was not on the task kicker row:\n%s", view)
}

func stackedPathPayload() Payload {
	payload := testPayload()
	payload.Activity = nil
	for i := 0; i < 40; i++ {
		payload.Activity = append(payload.Activity, domain.Event{
			ID: int64(200 + i), EventType: domain.EventTypeComment, Body: "stacked-path comment",
		})
	}
	parent := int64(10)
	for i := 0; i < 8; i++ {
		payload.Tasks = append(payload.Tasks, domain.Task{
			ID: int64(20 + i), Title: "child extra", BucketKey: "dev", ParentID: &parent,
		})
	}
	return payload
}

func taskdetailDrive(t *testing.T, frame screenhost.Frame, screen Screen, key string) Screen {
	t.Helper()
	next, ok := screen.Update(frame, screentest.Key(key)).Screen.(Screen)
	if !ok {
		t.Fatalf("Update(%q) carried %T", key, screen)
	}
	return next
}

func taskdetailFocusID(screen Screen) screenlayout.ID {
	switch screen.focus {
	case FocusSubtasks:
		return sectionSubtasks
	case FocusActivity:
		return sectionActivity
	default:
		return sectionDetails
	}
}

func taskdetailPaintedLeaves(res screengrid.Result) []screenlayout.ID {
	var out []screenlayout.ID
	for _, p := range res.Placements {
		if p.Leaf && !p.Dropped {
			out = append(out, p.ID)
		}
	}
	return out
}

func taskdetailGrid(frame screenhost.Frame, screen Screen) screengrid.Result {
	kit := screen.bodyKit(frame)
	return screengrid.Render(kit, screen.grid, screenlayout.HostBox(kit), screen.bodyRoot(frame))
}

func deriveTaskdetailStackedWidth(t *testing.T, payload Payload) (stacked, side int) {
	t.Helper()
	const probeH = 60
	var lastSide int
	for w := 200; w >= 40; w-- {
		frame := screentest.FrameAt(t, w, probeH)
		res := taskdetailGrid(frame, New().Open(payload, frame))
		place, ok := res.Placement("taskdetail")
		if !ok {
			continue
		}
		if place.Arrangement == screenlayout.SideBySide {
			lastSide = w
			continue
		}
		if place.Arrangement == screenlayout.Stacked && lastSide != 0 {
			return w, lastSide
		}
	}
	t.Fatal("taskdetail never reported Stacked below a SideBySide width at height 60")
	return 0, 0
}

func taskdetailSamePainted(want map[screenlayout.ID]bool, got []screenlayout.ID) bool {
	if len(got) != len(want) {
		return false
	}
	for _, id := range got {
		if !want[id] {
			return false
		}
	}
	return true
}
