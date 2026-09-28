package studio

import (
	"context"
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/config"
	"omakiten/internal/config/bundledraft"
	"omakiten/internal/contract"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

type applyMemoryEditor struct {
	path   string
	bundle config.Bundle
	saves  int
}

func (e *applyMemoryEditor) Path() string        { return e.path }
func (e *applyMemoryEditor) SetPath(path string) { e.path = path }
func (e *applyMemoryEditor) ConfigDir() string   { return "" }
func (e *applyMemoryEditor) RootDir() string     { return "" }
func (e *applyMemoryEditor) Load() (config.Bundle, error) {
	return bundledraft.CloneBundle(e.bundle), nil
}
func (e *applyMemoryEditor) LoadPlan() (config.Bundle, string, map[string]string, error) {
	return bundledraft.CloneBundle(e.bundle), "hash", map[string]string{e.path: "hash"}, nil
}
func (e *applyMemoryEditor) Hash() (string, error) { return "hash", nil }
func (e *applyMemoryEditor) Apply(_ context.Context, bundle config.Bundle, _ map[string]string, mutate func(*config.Bundle) error) (config.Bundle, error) {
	next := bundledraft.CloneBundle(bundle)
	if mutate != nil {
		if err := mutate(&next); err != nil {
			return config.Bundle{}, err
		}
	}
	e.saves++
	e.bundle = next
	return bundledraft.CloneBundle(next), nil
}

func applyTestBundle() config.Bundle {
	tru := true
	return config.Bundle{
		Version: 1,
		Kit:     config.Kit{ID: 1, Key: "omakase", Name: "Omakase"},
		Config: config.Settings{
			Output:   config.OutputSettings{JSONMinified: true, OmitEmpty: true},
			Workflow: config.WorkflowSettings{Active: "omakase"},
			Theme:    config.ThemeSettings{Active: "default"},
			Agent: config.AgentSettings{
				RecentCommentLimit:        5,
				IncludeWorkflowInContinue: &tru,
				NextWorkLimit:             5,
				SimilarTaskLimit:          5,
			},
			TUI:              config.TUISettings{TokenBadge: config.TokenBadgeThresholds{YellowAt: 150, RedAt: 400}},
			TemplateDefaults: []string{"task"},
			Priorities: []config.PriorityDefinition{
				{ID: 1, Value: "low"},
				{ID: 2, Value: "normal", Default: true},
				{ID: 3, Value: "high"},
			},
			Severities: []config.SeverityDefinition{
				{ID: 1, Value: "info"},
				{ID: 2, Value: "warning", Default: true},
				{ID: 3, Value: "error"},
			},
			Views: config.ViewSettings{
				Board:        config.BoardViewSettings{Sort: config.SortSettings{Field: "created_at", Order: "desc"}},
				Table:        config.TableViewSettings{Sort: config.SortSettings{Field: "created_at", Order: "desc"}},
				Graph:        config.GraphViewSettings{Sort: config.SortSettings{Field: "id", Order: "asc"}},
				Logs:         config.LogsViewSettings{Sort: config.SortSettings{Order: "desc"}, Limit: 50, WindowDays: 30},
				TaskActivity: config.TaskActivityViewSettings{Sort: config.SortSettings{Order: "asc"}},
			},
			SQLite:    config.SQLiteSettings{BusyTimeoutMs: 5000, CacheSizeKB: 1024},
			Solutions: config.SolutionsSettings{DefaultTopLimit: 10, MaxTopLimit: 100},
			Backup:    config.BackupSettings{RetentionCount: 5},
			Events: config.EventsSettings{
				DefaultRecentLimit: 50,
				Defaults:           config.EventChannelSettings{Log: &tru, Broadcast: &tru, Hook: &tru},
			},
			Search:      config.SearchSettings{Stopwords: []string{"and", "the"}},
			TagSynonyms: map[string]string{"golang": "go"},
		},
		Workflows: []config.Workflow{{
			ID:  1,
			Key: "omakase", Name: "Omakase",
			Buckets: []config.Bucket{
				{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
				{ID: 2, Key: "done", Name: "Done", Position: 2},
			},
			Transitions: []config.Transition{{From: 1, To: 2}},
		}},
		Skills:      []config.Skill{{Slug: "code"}},
		AllSkills:   []config.Skill{{Slug: "code"}},
		Personas:    []config.Persona{{Slug: "builder", SkillRepertoire: []string{"code"}}},
		AllPersonas: []config.Persona{{Slug: "builder", SkillRepertoire: []string{"code"}}},
		Commands:    map[string]config.CommandSpec{"task": {Persona: "builder", Skills: []string{"code"}}},
		Surfaces:    config.CanonicalSurfaceTable(),
	}
}

func applyTestScreen(t *testing.T, id screenhost.ID, editor contract.BundleEditor) (Screen, screenhost.Frame) {
	t.Helper()
	frame := screentest.FrameAt(t, 120, 40)
	screen := New().Bind(id, Deps{Ctx: context.Background(), Editor: editor, OpenDraft: openFixtureBundleDraft, Workflow: studioBenchWorkflow()})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	return screen, frame
}

func TestStudioApplyOverlayTwoPressAndMutationInvalidatesArm(t *testing.T) {
	t.Parallel()
	editor := &applyMemoryEditor{path: "omakiten.yaml", bundle: applyTestBundle()}
	screen, frame := applyTestScreen(t, screenhost.StudioCommands, editor)
	if err := screen.studioDraft.RenameBucket(1, "Inbox").ValidationError; err != nil {
		t.Fatalf("RenameBucket: %v", err)
	}

	screen = studioDrive(t, screen, frame, "ctrl+s")
	if !screen.ApplyOverlayOpen() {
		t.Fatal("first ctrl+s did not open the apply overlay")
	}
	state := screen.State()
	if !state.ApplyArmed || state.ApplyConfirmation == "" {
		t.Fatalf("first ctrl+s did not arm: armed=%v confirmation=%q", state.ApplyArmed, state.ApplyConfirmation)
	}
	if editor.saves != 0 {
		t.Fatalf("first ctrl+s wrote bundle, saves=%d", editor.saves)
	}
	armed := state.ApplyConfirmation

	if err := screen.studioDraft.RenameBucket(1, "Incoming").ValidationError; err != nil {
		t.Fatalf("RenameBucket mutation: %v", err)
	}
	screen = studioDrive(t, screen, frame, "ctrl+s")
	if editor.saves != 0 {
		t.Fatalf("mutated candidate reused stale arm, saves=%d", editor.saves)
	}
	if screen.State().ApplyConfirmation == armed {
		t.Fatal("draft mutation did not replace the confirmation hash")
	}

	screen = studioDrive(t, screen, frame, "ctrl+s")
	if editor.saves != 1 {
		t.Fatalf("second matching ctrl+s saves=%d, want 1", editor.saves)
	}
	if screen.ApplyOverlayOpen() {
		t.Fatal("successful Apply left the overlay open")
	}
	if screen.studioDraft.Report().Dirty {
		t.Fatal("successful Apply left the draft dirty")
	}
}

func TestStudioApplyOverlayCommandsCtrlSDoesNotNavigate(t *testing.T) {
	t.Parallel()
	editor := &applyMemoryEditor{path: "omakiten.yaml", bundle: applyTestBundle()}
	screen, frame := applyTestScreen(t, screenhost.StudioCommands, editor)
	if err := screen.studioDraft.RenameBucket(1, "Inbox").ValidationError; err != nil {
		t.Fatalf("RenameBucket: %v", err)
	}
	outcome := screen.Update(frame, screentest.Key("ctrl+s"))
	next, ok := outcome.Screen.(Screen)
	if !ok {
		t.Fatalf("Update carried %T", outcome.Screen)
	}
	if outcome.Action.Kind == screenhost.ActionNavigate {
		t.Fatalf("Commands ctrl+s navigated to %s; want overlay Stay", outcome.Action.Target)
	}
	if !next.ApplyOverlayOpen() {
		t.Fatal("Commands ctrl+s did not open the apply overlay")
	}
	if editor.saves != 0 {
		t.Fatalf("Commands ctrl+s applied, saves=%d", editor.saves)
	}
	view := next.Bind(screenhost.StudioCommands, Deps{Ctx: context.Background(), Editor: editor, OpenDraft: openFixtureBundleDraft}).View(frame)
	if !strings.Contains(view, "APPLY CANDIDATE") {
		t.Fatalf("overlay view missing APPLY CANDIDATE:\n%s", view)
	}
}

func TestStudioApplyOverlayEscMutatesNoDraft(t *testing.T) {
	t.Parallel()
	editor := &applyMemoryEditor{path: "omakiten.yaml", bundle: applyTestBundle()}
	screen, frame := applyTestScreen(t, screenhost.StudioCommands, editor)
	if err := screen.studioDraft.RenameBucket(1, "Inbox").ValidationError; err != nil {
		t.Fatalf("RenameBucket: %v", err)
	}
	before := screen.studioDraft.Report().Candidate.Workflows[0].Buckets[0].Name
	screen = studioDrive(t, screen, frame, "ctrl+s")
	screen = studioDrive(t, screen, frame, "esc")
	if screen.ApplyOverlayOpen() {
		t.Fatal("esc did not dismiss the overlay")
	}
	if screen.State().ApplyArmed {
		t.Fatal("esc left the arm live")
	}
	if editor.saves != 0 {
		t.Fatalf("esc wrote bundle, saves=%d", editor.saves)
	}
	if got := screen.studioDraft.Report().Candidate.Workflows[0].Buckets[0].Name; got != before {
		t.Fatalf("esc mutated draft bucket name %q -> %q", before, got)
	}
}

func TestStudioApplyOverlayCleanAndValidationFailure(t *testing.T) {
	t.Parallel()
	t.Run("clean", testStudioApplyClean)
	t.Run("validation", testStudioApplyValidation)
	t.Run("blocked", testStudioApplyBlocked)
}

func TestStudioApplyRouteSanitizesWarningsAndStatus(t *testing.T) {
	t.Parallel()
	editor := &applyMemoryEditor{path: "omakiten.yaml", bundle: applyTestBundle()}
	screen, frame := applyTestScreen(t, screenhost.StudioCommands, editor)
	hostile := "warning 漢字 \x1b[31mred\x1b]0;owned\a \x00\u009b31m\u009d"
	screen.studioApplyOpen = true
	screen.studioApplyMsg = hostile
	screen.projection.FlowWarnings = []string{hostile}
	plain := ansi.Strip(screen.View(frame))
	if strings.Contains(plain, "owned") {
		t.Fatalf("Studio Apply retained OSC payload: %q", plain)
	}
	for _, r := range plain {
		if r != '\n' && unicode.IsControl(r) {
			t.Fatalf("Studio Apply retained control U+%04X: %q", r, plain)
		}
	}
	if !strings.Contains(plain, "漢字") {
		t.Fatalf("Studio Apply lost harmless Unicode: %q", plain)
	}
}

func testStudioApplyClean(t *testing.T) {
	t.Parallel()
	editor := &applyMemoryEditor{path: "omakiten.yaml", bundle: applyTestBundle()}
	screen, frame := applyTestScreen(t, screenhost.StudioCommands, editor)
	screen = studioDrive(t, screen, frame, "ctrl+s")
	if !screen.ApplyOverlayOpen() {
		t.Fatal("clean ctrl+s did not open the overlay")
	}
	if screen.State().ApplyArmed {
		t.Fatal("clean candidate was armed")
	}
	view := screen.View(frame)
	if !strings.Contains(view, "APPLY CANDIDATE") {
		t.Fatalf("clean overlay missing kicker:\n%s", view)
	}
	if editor.saves != 0 {
		t.Fatalf("clean apply wrote bundle, saves=%d", editor.saves)
	}
}

func testStudioApplyValidation(t *testing.T) {
	t.Parallel()
	editor := &applyMemoryEditor{path: "omakiten.yaml", bundle: applyTestBundle()}
	screen, frame := applyTestScreen(t, screenhost.StudioCommands, editor)
	report := screen.studioDraft.Mutate(func(bundle *config.Bundle) error {
		bundle.Workflows[0].Key = ""
		return nil
	})
	if report.ValidationError == nil {
		t.Fatal("empty workflow key did not fail ValidateBundle")
	}
	screen = studioDrive(t, screen, frame, "ctrl+s")
	if !screen.ApplyOverlayOpen() {
		t.Fatal("invalid candidate did not open the overlay")
	}
	if screen.State().ApplyArmed {
		t.Fatal("invalid candidate was armed")
	}
	if editor.saves != 0 {
		t.Fatalf("blocked-by-validation apply wrote bundle, saves=%d", editor.saves)
	}
	view := screen.View(frame)
	if !strings.Contains(view, "apply disabled") {
		t.Fatalf("validation-failure overlay missing apply disabled:\n%s", view)
	}
}

func testStudioApplyBlocked(t *testing.T) {
	t.Parallel()
	bundle := applyTestBundle()
	bundle.SourcePaths = []string{"omakiten.yaml", "workflows.yaml"}
	editor := &applyMemoryEditor{path: "omakiten.yaml", bundle: bundle}
	screen, frame := applyTestScreen(t, screenhost.StudioCommands, editor)
	if err := screen.studioDraft.RenameBucket(1, "Inbox").ValidationError; err != nil {
		t.Fatalf("RenameBucket: %v", err)
	}
	screen = studioDrive(t, screen, frame, "ctrl+s")
	if !screen.ApplyOverlayOpen() {
		t.Fatal("blocked candidate did not open the overlay")
	}
	if screen.State().ApplyArmed {
		t.Fatal("blocked candidate was armed")
	}
	if editor.saves != 0 {
		t.Fatalf("blocked apply wrote bundle, saves=%d", editor.saves)
	}
	if !strings.Contains(screen.State().ApplyMessage, "apply disabled") {
		t.Fatalf("blocked overlay message = %q", screen.State().ApplyMessage)
	}
}
