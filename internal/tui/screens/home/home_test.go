package home

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/domain"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/screenhost"
)

func TestProjectIntents(t *testing.T) {
	project := domain.Project{ID: 7, Name: "Alpha", Slug: "alpha", RootPath: "/work/alpha"}
	s := New().Apply(Result{Generation: 0, Payload: Payload{Projects: []domain.Project{project}}})

	for _, tc := range []struct {
		key       string
		kind      screenhost.ActionKind
		projectID int64
	}{
		{key: "enter", kind: screenhost.ActionSelectProject, projectID: 7},
		{key: "n", kind: screenhost.ActionCreateProject},
		{key: "e", kind: screenhost.ActionEditProject, projectID: 7},
	} {
		t.Run(tc.key, func(t *testing.T) {
			out := s.Update(testFrame(80, 24), key(tc.key))
			if out.Action.Kind != tc.kind || out.Action.ProjectID != tc.projectID {
				t.Fatalf("action = %+v, want kind %v project %d", out.Action, tc.kind, tc.projectID)
			}
		})
	}
}

func TestDeleteIntentArmsThenConfirms(t *testing.T) {
	project := domain.Project{ID: 7, Name: "Alpha", Slug: "alpha"}
	counters := domain.ProjectDeleteCounters{Tasks: 3, Comments: 2}
	s := New().Apply(Result{Payload: Payload{Projects: []domain.Project{project}}})

	out := s.Update(testFrame(80, 24), key("d"))
	if out.Action.Kind != screenhost.ActionPrepareProjectDelete || out.Action.ProjectID != 7 {
		t.Fatalf("prepare action = %+v", out.Action)
	}
	s = out.Screen.(Screen).ArmDelete(7, counters)
	out = s.Update(testFrame(80, 24), key("d"))
	if out.Action.Kind != screenhost.ActionDeleteProject || out.Action.ProjectID != 7 || out.Action.DeleteCounters != counters {
		t.Fatalf("delete action = %+v", out.Action)
	}
	if out.Screen.(Screen).ArmedProjectID() != 0 {
		t.Fatal("delete arm survived confirmation")
	}
}

func TestDeleteCancelClearsArm(t *testing.T) {
	s := New().Apply(Result{Payload: Payload{Projects: []domain.Project{{ID: 7, Name: "Alpha"}}}}).ArmDelete(7, domain.ProjectDeleteCounters{Tasks: 1})
	out := s.Update(testFrame(80, 24), key("esc"))
	if out.Action.Kind != screenhost.ActionNone || out.Screen.(Screen).ArmedProjectID() != 0 {
		t.Fatalf("cancel outcome = %+v, armed=%d", out.Action, out.Screen.(Screen).ArmedProjectID())
	}
}

func TestRefreshAndStaleResult(t *testing.T) {
	s := New().Apply(Result{Payload: Payload{Projects: []domain.Project{{ID: 1, Name: "Old"}}}})
	out := s.Update(testFrame(80, 24), key("ctrl+h"))
	loading := out.Screen.(Screen)
	if out.Action.Kind != screenhost.ActionReload || out.Action.Generation == 0 || !loading.IsLoading() {
		t.Fatalf("reload outcome = %+v loading=%v", out.Action, loading.IsLoading())
	}

	stale := loading.Apply(Result{Generation: out.Action.Generation - 1, Payload: Payload{Projects: []domain.Project{{ID: 2, Name: "Stale"}}}})
	if stale.Projects()[0].Name != "Old" || !stale.IsLoading() {
		t.Fatalf("stale result applied: projects=%+v loading=%v", stale.Projects(), stale.IsLoading())
	}
	fresh := stale.Apply(Result{Generation: out.Action.Generation, Payload: Payload{Projects: []domain.Project{{ID: 3, Name: "Fresh"}}}})
	if fresh.Projects()[0].Name != "Fresh" || fresh.IsLoading() {
		t.Fatalf("fresh result not applied: projects=%+v loading=%v", fresh.Projects(), fresh.IsLoading())
	}
}

func TestResizeKeepsSelectionVisible(t *testing.T) {
	projects := make([]domain.Project, 12)
	for i := range projects {
		projects[i] = domain.Project{ID: int64(i + 1), Name: strings.Repeat("project ", 3)}
	}
	s := New().Apply(Result{Payload: Payload{Projects: projects}})
	for range 11 {
		s = s.Update(testFrame(80, 40), key("down")).Screen.(Screen)
	}
	s = s.Lifecycle(testFrame(48, 12), screenhost.LifecycleResize).Screen.(Screen)
	if s.Cursor() != 11 || s.Scroll() == 0 {
		t.Fatalf("cursor=%d scroll=%d, want last selection visible after resize", s.Cursor(), s.Scroll())
	}
}

func TestEmptyLoadingAndErrorProjection(t *testing.T) {
	frame := testFrame(80, 24)
	if got := ansi.Strip(New().View(frame)); !strings.Contains(got, "No projects registered") {
		t.Fatalf("empty view missing guidance:\n%s", got)
	}
	loading := New().Loading(1)
	if got := ansi.Strip(loading.View(frame)); !strings.Contains(got, "Loading") {
		t.Fatalf("loading view missing state:\n%s", got)
	}
	failed := loading.Apply(Result{Generation: 1, Err: errors.New("projects unavailable")})
	if got := ansi.Strip(failed.View(frame)); !strings.Contains(got, "projects unavailable") {
		t.Fatalf("error view missing error:\n%s", got)
	}
}

// TestHomeFailedStateRendersViaScreenstate falsifies the blank-screen hazard
// (#126545): if View's liveness guard omits err while cardsBlock skips on it,
// the screen paints nothing when Failed is live.
func TestHomeFailedStateRendersViaScreenstate(t *testing.T) {
	frame := testFrame(100, 50)
	screen := New().Loading(1).Apply(Result{Generation: 1, Err: errors.New("projects \x1b[31munavailable\x1b[0m\nretry")})
	view := ansi.Strip(screen.View(frame))
	if view == "" {
		t.Fatal("failed view is blank")
	}
	if !strings.Contains(view, "[ERROR]") {
		t.Fatalf("failed view missing error badge:\n%s", view)
	}
	if !strings.Contains(view, "projects unavailable retry") {
		t.Fatalf("failed view missing sanitized, normalized error detail:\n%s", view)
	}
}

func TestContractDeclarationsAndOverlayConfirm(t *testing.T) {
	s := New().Apply(Result{Payload: Payload{Projects: []domain.Project{{ID: 7, Name: "Alpha"}}}}).ArmDelete(7, domain.ProjectDeleteCounters{Tasks: 2})
	if s.ID() != screenhost.Home || !s.OwnsKey(key("d")) || !s.OwnsFooter() || s.Generation() != 0 {
		t.Fatal("home contract declarations changed")
	}
	if len(s.Footer(testFrame(80, 24))) == 0 || len(s.Help(testFrame(80, 24))) != 1 {
		t.Fatal("home footer/help declarations missing")
	}
	out := s.ConfirmDelete()
	if out.Action.Kind != screenhost.ActionDeleteProject || out.Action.Generation != 1 || !out.Screen.(Screen).IsLoading() {
		t.Fatalf("overlay confirm = %+v", out)
	}
	if got := New().ConfirmDelete(); got.Action.Kind != screenhost.ActionNone {
		t.Fatalf("empty confirm action = %+v", got.Action)
	}
	if len(New().Footer(testFrame(80, 24))) == 0 {
		t.Fatal("empty footer missing create action")
	}
}

func TestRenderingBoundsAndHelpers(t *testing.T) {
	s := New().Apply(Result{Payload: Payload{
		Projects: []domain.Project{{ID: 1, Name: "", Slug: "an-extremely-long-slug", RootPath: "/deep/path/to/a/very-long-leaf-name"}},
		Tags:     map[int64][]domain.Tag{1: {{Name: "an-extremely-long-unbroken-tag"}}}, Pending: map[int64]int{1: 1},
	}})
	for _, size := range [][2]int{{30, 10}, {80, 24}, {120, 40}} {
		frame := testFrame(size[0], size[1])
		view := ansi.Strip(s.View(frame))
		if got := ansi.StringWidth(widestLine(view)); got > frame.Width() {
			t.Fatalf("home width = %d, terminal = %d", got, frame.Width())
		}
		assertHomeCardBorders(t, view)
	}
	for _, tc := range []struct {
		value string
		width int
	}{
		{"abcdef", 1}, {"abcdef", 4}, {"ok", 4}, {"abcdef", 0},
	} {
		if got := screenkit.Truncate(tc.value, tc.width); tc.width > 0 && ansi.StringWidth(got) > tc.width {
			t.Fatalf("screenkit.Truncate(%q, %d) = %q", tc.value, tc.width, got)
		}
	}
	for _, width := range []int{0, 2, 8, 20} {
		got := screenkit.TruncatePath("/alpha/bravo/charlie", width)
		if width > 0 && ansi.StringWidth(got) > width {
			t.Fatalf("screenkit.TruncatePath width %d = %q", width, got)
		}
	}
	if got := wrapWords("", 0); len(got) != 1 {
		t.Fatalf("wrapWords empty = %v", got)
	}
}

func assertHomeCardBorders(t *testing.T, view string) {
	t.Helper()
	for _, line := range strings.Split(view, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "│┌") && !strings.HasSuffix(line, "┐│") {
			t.Fatalf("project card border is clipped: %q", line)
		}
	}
}

func widestLine(value string) string {
	widest := ""
	for _, line := range strings.Split(value, "\n") {
		if ansi.StringWidth(line) > ansi.StringWidth(widest) {
			widest = line
		}
	}
	return widest
}

func key(value string) tea.KeyMsg {
	switch value {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "ctrl+h":
		return tea.KeyMsg{Type: tea.KeyCtrlH}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)}
	}
}

func testFrame(width, height int) screenhost.Frame {
	styles := screenkit.Styles{
		Panel:        lipgloss.NewStyle().Border(lipgloss.NormalBorder()),
		Separator:    lipgloss.NewStyle(),
		Info:         lipgloss.NewStyle(),
		Hint:         lipgloss.NewStyle(),
		HintAccent:   lipgloss.NewStyle(),
		HintBox:      lipgloss.NewStyle(),
		Empty:        lipgloss.NewStyle(),
		Error:        lipgloss.NewStyle(),
		BadgeLow:     lipgloss.NewStyle(),
		BadgeNormal:  lipgloss.NewStyle(),
		BadgeBlocker: lipgloss.NewStyle(),
		BadgeInfo:    lipgloss.NewStyle(),
		Card:         lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Padding(0, 1),
		CardSelected: lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Padding(0, 1),
	}
	text := func(k string) string {
		values := map[string]string{
			"tui.empty.home_no_projects":      "no projects",
			"tui.empty.home_no_projects_full": "No projects registered.",
			"tui.home.register_with":          "Register one with:",
			"tui.home.okt_init_example":       "  okt init --name MyProject --slug my-project",
			"tui.home.then_reopen_prefix":     "Then reopen ",
			"tui.home.okt_tui_cmd":            "okt tui",
			"tui.home.then_reopen_suffix":     ".",
			"tui.home.projects_kicker_fmt":    "// PROJECTS · %d",
			"tui.loading.projects":            "Loading projects…",
			"tui.stat.error_badge":            "[ERROR]",
			"tui.badge.open":                  "OPEN",
		}
		if value := values[k]; value != "" {
			return value
		}
		return k
	}
	return screenhost.NewFrame(screenhost.FrameOptions{Width: width, Height: height, Styles: styles, Text: text})
}
