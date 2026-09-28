package screenhost

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/tui/components/screenkit"
)

type stubScreen struct{ id ID }

func (s stubScreen) ID() ID                                  { return s.id }
func (s stubScreen) Update(Frame, tea.Msg) Outcome           { return Stay(s, nil) }
func (s stubScreen) View(Frame) string                       { return string(s.id) }
func (s stubScreen) Footer(Frame) []FooterBinding            { return nil }
func (s stubScreen) Help(Frame) []HelpGroup                  { return nil }
func (s stubScreen) Lifecycle(Frame, LifecycleEvent) Outcome { return Stay(s, nil) }

func TestFrameIsAnImmutableHostSnapshot(t *testing.T) {
	t.Parallel()

	frame := NewFrame(FrameOptions{
		Width:       120,
		Height:      40,
		ProjectID:   42,
		ProjectSlug: "omakiten",
		Status:      "ready",
		Focused:     true,
	})

	if frame.Width() != 120 || frame.Height() != 40 {
		t.Fatalf("frame size = %dx%d, want 120x40", frame.Width(), frame.Height())
	}
	if frame.ProjectID() != 42 || frame.ProjectSlug() != "omakiten" {
		t.Fatalf("frame project = (%d, %q), want (42, omakiten)", frame.ProjectID(), frame.ProjectSlug())
	}
	if frame.Status() != "ready" || !frame.Focused() {
		t.Fatalf("frame status/focus = (%q, %v), want (ready, true)", frame.Status(), frame.Focused())
	}

	options := FrameOptions{Width: 80, ProjectSlug: "before"}
	snapshot := NewFrame(options)
	options.Width = 10
	options.ProjectSlug = "after"
	if snapshot.Width() != 80 || snapshot.ProjectSlug() != "before" {
		t.Fatalf("frame changed through source options: width=%d slug=%q", snapshot.Width(), snapshot.ProjectSlug())
	}
}

func TestOutcomeCarriesSemanticHostIntent(t *testing.T) {
	t.Parallel()

	screen := stubScreen{id: TasksBoard}
	outcome := Navigate(screen, StatsInsights, nil)
	if outcome.Screen.ID() != TasksBoard {
		t.Fatalf("outcome screen = %q, want %q", outcome.Screen.ID(), TasksBoard)
	}
	if outcome.Action.Kind != ActionNavigate || outcome.Action.Target != StatsInsights {
		t.Fatalf("outcome action = %+v, want navigate to %q", outcome.Action, StatsInsights)
	}

	outcome = Reload(screen, nil)
	if outcome.Action.Kind != ActionReload {
		t.Fatalf("reload action = %v, want ActionReload", outcome.Action.Kind)
	}

	outcome = SetStatus(screen, "saved", nil)
	if outcome.Action.Kind != ActionSetStatus || outcome.Action.Status != "saved" {
		t.Fatalf("status action = %+v, want saved status", outcome.Action)
	}

	outcome = OpenTask(screen, 1980, nil)
	if outcome.Action.Kind != ActionOpenTask || outcome.Action.TaskID != 1980 {
		t.Fatalf("open-task action = %+v, want task 1980", outcome.Action)
	}

	outcome = MoveTask(screen, 1980, nil)
	if outcome.Action.Kind != ActionMoveTask || outcome.Action.TaskID != 1980 {
		t.Fatalf("move-task action = %+v, want task 1980", outcome.Action)
	}
}

func TestRegistryRejectsDuplicateStableIdentity(t *testing.T) {
	t.Parallel()

	factory := func(Host) Screen { return stubScreen{id: TasksBoard} }
	spec := DescriptorSpec{
		ID:        TasksBoard,
		Placement: Placement{Top: TopTasks, TopOrder: 1, SubOrder: 1, Cyclic: true},
		TopLabel:  "TASKS", SubLabel: "board",
		Palette:  PaletteMetadata{Route: "tasks.board", Code: "11", TitleKey: "tasks_board"},
		Factory:  factory,
		Chrome:   Chrome{Navigation: true, Footer: true, Help: true},
		Reload:   ReloadBundle,
		HelpKeys: []string{"tasks_board"},
	}
	if _, err := NewRegistry([]DescriptorSpec{spec, spec}); err == nil {
		t.Fatal("NewRegistry accepted duplicate screen IDs")
	}

	other := spec
	other.ID = TasksTable
	if _, err := NewRegistry([]DescriptorSpec{spec, other}); err == nil {
		t.Fatal("NewRegistry accepted duplicate palette routes/codes")
	}
}

func TestRegistryRejectsDescriptorMetadataDrift(t *testing.T) {
	t.Parallel()

	factory := func(Host) Screen { return stubScreen{id: TasksBoard} }
	valid := DescriptorSpec{
		ID:        TasksBoard,
		Placement: Placement{Top: TopTasks, TopOrder: 1, SubOrder: 1, Cyclic: true},
		TopLabel:  "TASKS", SubLabel: "board",
		Palette:  PaletteMetadata{Route: string(TasksBoard), Code: "11", TitleKey: "tasks_board"},
		Factory:  factory,
		Chrome:   Chrome{Navigation: true, Footer: true, Help: true},
		Reload:   ReloadBundle,
		HelpKeys: []string{"tasks_board"},
	}

	cases := map[string]func(*DescriptorSpec){
		"palette route differs from stable ID": func(spec *DescriptorSpec) { spec.Palette.Route = "other.route" },
		"cyclic screen has no top label":       func(spec *DescriptorSpec) { spec.TopLabel = "" },
		"cyclic screen has no sub label":       func(spec *DescriptorSpec) { spec.SubLabel = "" },
		"help chrome has no help key":          func(spec *DescriptorSpec) { spec.HelpKeys = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			spec := valid
			mutate(&spec)
			if _, err := NewRegistry([]DescriptorSpec{spec}); err == nil {
				t.Fatal("NewRegistry accepted drifting descriptor metadata")
			}
		})
	}
}

// TestFrameCarriesHostChromeAndRenderKit pins the render context a screen can
// only obtain from the host: the chrome-row budget it cannot measure (the
// header and status badge render outside the screen boundary) and the theme /
// catalog projection it paints with. Geometry has a single source of truth —
// the kit reads it back off the frame.
func TestFrameCarriesHostChromeAndRenderKit(t *testing.T) {
	t.Parallel()

	frame := NewFrame(FrameOptions{
		Width:      120,
		Height:     40,
		ChromeRows: 9,
		Styles:     screenkit.Styles{},
		Text:       func(key string) string { return "resolved:" + key },
	})

	if frame.ChromeRows() != 9 {
		t.Fatalf("frame chrome rows = %d, want 9", frame.ChromeRows())
	}
	kit := frame.Kit()
	if kit.Width != 120 || kit.Height != 40 || kit.ChromeRows != 9 {
		t.Fatalf("kit geometry = %dx%d chrome=%d, want 120x40 chrome=9", kit.Width, kit.Height, kit.ChromeRows)
	}
	if got := kit.ViewportRows(5); got != 40-(9+1+5+2) {
		t.Fatalf("kit viewport rows = %d, want the chrome-aware budget", got)
	}
	if got := frame.Text("tui.kicker.stats"); got != "resolved:tui.kicker.stats" {
		t.Fatalf("frame text = %q, want the host catalog literal", got)
	}
}

// TestFrameWithoutCatalogDegradesToKeys keeps a headless frame renderable:
// screens resolve labels through the frame, so an unwired catalog must yield
// the key rather than panicking on a nil resolver.
func TestFrameWithoutCatalogDegradesToKeys(t *testing.T) {
	t.Parallel()

	frame := NewFrame(FrameOptions{})
	if got := frame.Text("tui.footer.refresh"); got != "tui.footer.refresh" {
		t.Fatalf("frame text = %q, want the key", got)
	}
	if got := frame.Kit().AvailableWidth(); got != 116 {
		t.Fatalf("unmeasured frame width = %d, want the 120-cell assumption", got)
	}
}
