package projectresume

import (
	"fmt"
	"strings"
	"testing"

	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

func TestProjectResumeScreenRendersFacadePayload(t *testing.T) {
	frame := screentest.FrameAt(t, 100, 50)
	screen := New().Apply(Payload{
		TaskBuckets: []BucketCount{
			{BucketKey: "backlog", Name: "Backlog", Count: 2},
			{BucketKey: "dev", Name: "Development", Count: 1},
		},
		LikelyNextWork: []TaskSummary{
			{ID: 11, Title: "Ship resume UI", BucketKey: "backlog", Priority: "high"},
		},
		BlockedWork: []TaskSummary{
			{ID: 22, Title: "Blocked by deps", BucketKey: "dev"},
		},
		Dependencies: []DependencySummary{
			{TaskID: 22, DependsOnTaskID: 11},
		},
		NextStepPrompt: "Choose a likely next task",
	})
	// Enter is required because production never paints a hosted screen that
	// has not gone through it. pushScreen stores the instance after
	// LifecycleEnter (screen_host.go:710); a WindowSizeMsg stores it after
	// LifecycleResize (model.go:187); navigation reset stores it after
	// LifecycleEnter (model.go:641). renderCurrentView then Factory-resolves
	// that stored instance — it does not build a fresh one. Apply+View
	// without Enter is a cycle the host cannot produce. Reintroducing
	// composition in View to keep the old test would make M6a measure a
	// screen that still composes on paint, which is the thing C.1 exists
	// to undo.
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	view := screen.View(frame)
	for _, want := range []string{"Backlog", "Ship resume UI", "Blocked by deps", "#22", "#11", "Choose a likely next task"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestProjectResumeMountsBodyAsGridCellAtPanelWidth(t *testing.T) {
	frame := screentest.FrameAt(t, 80, 24)
	screen := New().Apply(budgetPayload())
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)

	result := screen.gridResult(frame)
	placement, ok := result.Placement(bodySpec.ID)
	if !ok || !placement.Leaf {
		t.Fatalf("body placement = %+v, found=%v; want Cell leaf", placement, ok)
	}
	wantWidth := screen.panelBox(frame.Kit()).Width
	if placement.Box.Width != wantWidth {
		t.Fatalf("body width = %d, want panel width %d", placement.Box.Width, wantWidth)
	}
	if screen.bodyWidth != wantWidth {
		t.Fatalf("composed width = %d, want arranged width %d", screen.bodyWidth, wantWidth)
	}
}

func TestProjectResumeGridPreservesCursorAndResyncsOnResize(t *testing.T) {
	wide := screentest.FrameAt(t, 100, 24)
	screen := New().Apply(budgetPayload())
	screen = screen.Lifecycle(wide, screenhost.LifecycleEnter).Screen.(Screen)
	screen = screen.Update(wide, screentest.Key("G")).Screen.(Screen)
	if screen.Scroll() == 0 {
		t.Fatal("G did not move the loaded body window")
	}
	wantCursor := screen.grid.Layout().Cursor(bodySpec.ID)

	narrow := screentest.FrameAt(t, 80, 20)
	screen = screen.Lifecycle(narrow, screenhost.LifecycleResize).Screen.(Screen)
	if got := screen.grid.Layout().Cursor(bodySpec.ID); got != wantCursor {
		t.Fatalf("cursor after resize = %d, want %d", got, wantCursor)
	}
	if screen.Scroll() == 0 {
		t.Fatal("resize discarded the loaded body offset")
	}
}

func TestProjectResumeKeysOpenReloadAndBack(t *testing.T) {
	frame := screentest.FrameAt(t, 80, 24)
	screen := New().Apply(Payload{})
	if got := screen.Update(frame, screentest.Key("r")).Action.Kind; got != screenhost.ActionReload {
		t.Fatalf("r action = %v, want reload", got)
	}
	if got := screen.Update(frame, screentest.Key("esc")).Action.Kind; got != screenhost.ActionBack {
		t.Fatalf("esc action = %v, want back", got)
	}
	if !screen.OwnsKey(screentest.Key("r")) || !screen.OwnsKey(screentest.Key("j")) {
		t.Fatal("expected resume screen to own r and j")
	}
}

func TestProjectResumeLoadingPlaceholder(t *testing.T) {
	frame := screentest.FrameAt(t, 80, 24)
	view := New().View(frame)
	if view == "" {
		t.Fatal("expected computing placeholder")
	}
}

func TestProjectResumeFailedStateRendersViaScreenstate(t *testing.T) {
	frame := screentest.FrameAt(t, 100, 50)
	screen := New().Apply(Payload{Err: fmt.Errorf("resume \x1b[31mload failed\x1b[0m\nretry")})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	view := screentest.StripANSI(screen.View(frame))
	if view == "" {
		t.Fatal("failed view is blank")
	}
	if !strings.Contains(view, "[ERROR]") {
		t.Fatalf("failed view missing error badge:\n%s", view)
	}
	if !strings.Contains(view, "resume load failed retry") {
		t.Fatalf("failed view missing sanitized, normalized error detail:\n%s", view)
	}
}

func budgetPayload() Payload {
	const n = 80
	buckets := make([]BucketCount, 12)
	for i := range buckets {
		buckets[i] = BucketCount{
			BucketKey: fmt.Sprintf("bucket-%02d", i),
			Name:      fmt.Sprintf("Bucket %02d", i),
			Count:     (i + 1) * 3,
		}
	}
	likely := make([]TaskSummary, n)
	blocked := make([]TaskSummary, n)
	deps := make([]DependencySummary, n)
	for i := 0; i < n; i++ {
		likely[i] = TaskSummary{
			ID:        int64(i + 1),
			Title:     fmt.Sprintf("Likely next work item %03d long enough that the row is not a cheap empty cell", i+1),
			BucketKey: "backlog",
			Priority:  "high",
		}
		blocked[i] = TaskSummary{
			ID:        int64(1000 + i),
			Title:     fmt.Sprintf("Blocked work item %03d waiting on a dependency", i+1),
			BucketKey: "dev",
		}
		deps[i] = DependencySummary{TaskID: int64(1000 + i), DependsOnTaskID: int64(i + 1)}
	}
	return Payload{
		TaskBuckets:    buckets,
		LikelyNextWork: likely,
		BlockedWork:    blocked,
		Dependencies:   deps,
		NextStepPrompt: "Pick the next task from the likely-next list",
	}
}
