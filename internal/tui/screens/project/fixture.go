package project

import (
	"fmt"
	"strings"

	"omakiten/internal/domain"
	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screenhost"
)

// This file records the characterization baseline for the two screens this
// package owns — the project overview and its full-width description reader —
// through the shared three-geometry recorder.
//
// The overview is the interesting one for a layout migration: it has TWO
// layout branches (the meta/dashboard column beside the feed on a wide
// terminal, the three zones stacked on a narrow one) and the 80-column floor
// falls on the stacked side while 120 and 200 fall on the side-by-side side. So
// each overview recording captures both branches, and the states recorded move
// the two things the screen owns independently: the document offset shared by
// all three zones, and the activity cursor the feed zone walks.
//
// Everything the screens print comes from the payload. Markdown and activity
// cards paint through components/markdown and components/card with
// TrueColor forced, so there is no host paint callback and no path, clock
// reading or map on the render path.

// goldenRootPath and goldenName are deliberately long: the overview's meta
// panel gives its value column 16 cells at 80 and 90 at 200, and the reader
// gives it 60 and 180, so both values wrap at the floor and neither wraps at
// the widest terminal. The wrap column is the first thing a layout migration
// moves, which is exactly what a fixture that never wraps fails to record.
const (
	goldenRootPath = "/home/omakiten/Projects/personal/omakiten-characterization-baseline/checkout"
	goldenName     = "Omakiten Characterization Baseline (three-geometry screen recordings)"
)

// goldenDescription is long enough to overflow the reader's tallest window (40
// rows at 200x50) and wide enough in its opening paragraph to wrap at every
// recorded width, so the reader fixtures pin a scroll window rather than a
// document that happens to fit.
func goldenDescription() string {
	var b strings.Builder
	b.WriteString("The project reader has to survive a paragraph long enough that it wraps at every recorded width, because the wrap column is the first thing a layout migration moves.\n\n")
	for i := 1; i <= 60; i++ {
		fmt.Fprintf(&b, "%02d. description line %02d\n", i, i)
	}
	return b.String()
}

// fixtureBasePayload is the shared payload skeleton behavioural tests and
// goldens both describe. Tags and dashboard stay identical so the baseline and
// the assertions describe one screen.
func fixtureBasePayload() Payload {
	parentID := int64(11)
	return Payload{
		Project:     domain.ProjectContext{ID: 7, Name: "Demo", Slug: "demo", RootPath: "/tmp/demo"},
		Description: "ProjectDescriptionMarker",
		Tags:        []domain.Tag{{Name: "go", Label: "Go"}},
		Activity: []domain.Event{
			{ID: 21, EventType: domain.EventTypeComment, EntityType: domain.EventEntityProject, EntityID: 7, Body: "first project note"},
			{ID: 22, EventType: "task.updated", EntityType: domain.EventEntityTask, EntityID: 12},
		},
		Dashboard: Dashboard{
			Buckets:    []BucketCount{{Name: "Backlog", Count: 2}, {Name: "Done", Count: 1}},
			TotalTasks: 3, RootTasks: 2, SubTasks: 1, PlanCount: 1, PlanDone: 1, PlanTotal: 2,
		},
		Tasks: []domain.Task{{ID: 12, ParentID: &parentID}},
	}
}

// goldenPayload is fixtureBasePayload with a long name, a long root path, a
// long description and a feed deep enough to overflow every recorded terminal.
//
// Activity bodies stay short on purpose. The feed panel wraps its cards to the
// panel width AFTER flattenCards has measured their heights, so a card that
// wraps would put the cursor-follow arithmetic and the painted rows on
// different line counts — a state worth fixing, not worth freezing.
func goldenPayload() Payload {
	payload := fixtureBasePayload()
	payload.Project.Name = goldenName
	payload.Project.RootPath = goldenRootPath
	payload.Description = goldenDescription()
	payload.Activity = nil
	for i := 1; i <= 40; i++ {
		event := domain.Event{ID: int64(i), EventType: domain.EventTypeComment, EntityType: domain.EventEntityProject, EntityID: 7, Body: fmt.Sprintf("note %02d", i)}
		if i%4 == 0 {
			event = domain.Event{ID: int64(i), EventType: "task.updated", EntityType: domain.EventEntityTask, EntityID: int64(100 + i)}
		}
		payload.Activity = append(payload.Activity, event)
	}
	return payload
}

func goldenOverviewBuild(frame screenhost.Frame) screenhost.Screen {
	return screenfixture.Enter(New().Apply(Result{Payload: goldenPayload()}), frame)
}

func goldenReaderBuild(frame screenhost.Frame) screenhost.Screen {
	return screenfixture.Enter(NewForm().Apply(goldenPayload()), frame)
}

func overviewFixtureScenarios() []screenfixture.Scenario {
	return []screenfixture.Scenario{
		{
			// The overview as the host paints it on entry: the meta panel zone
			// holds focus, no card is selected and the document sits at row 0.
			// This is the fixture that records which of the two layout branches
			// each geometry takes.
			Name:  "project",
			Build: goldenOverviewBuild,
		},
		{
			// The one document offset all three zones share, moved a page and
			// four rows down from the meta zone. Paging from a zone that is NOT
			// the feed is the exact motion the screen used to refuse, so this is
			// the fixture a regression there would move.
			Name:  "project-scrolled",
			Build: goldenOverviewBuild,
			Keys:  []string{"pgdown", "j", "j", "j", "j"},
		},
		{
			// The feed zone with its cursor parked deep in the feed, reached the
			// way a user reaches it: two tabs to the feed, `G` to its last card,
			// then three steps back up. The window follows the selection, so this
			// records both the accented feed kicker and the document offset the
			// follow arithmetic produced — the one number the two layout branches
			// compute differently.
			//
			// `G` and back rather than a run of `j`: the widest recorded terminal
			// gives the document a 40-row window, so a cursor in the first dozen
			// cards is still inside the entry window there and the fixture would
			// record scroll 0 at 200x50 — a selection off its default cell that
			// proves nothing about the follow.
			Name:  "project-activity-cursor",
			Build: goldenOverviewBuild,
			Keys:  []string{"tab", "tab", "G", "k", "k", "k"},
		},
	}
}

func formFixtureScenarios() []screenfixture.Scenario {
	return []screenfixture.Scenario{
		{
			// The reader at rest: rendered markdown, top of the document. It
			// shares the overview's payload, so the same long name and root path
			// are recorded against the reader's much wider value column — which
			// is what makes the pair of fixtures a record of two layouts rather
			// than one.
			Name:  "project_form",
			Build: goldenReaderBuild,
		},
		{
			// The reader's other mode, scrolled: `M` flips the body to raw source
			// and the document is paged four rows past a page step. Mode and
			// offset are the reader's whole state, so this fixture and the one
			// above are the two ends of it.
			Name:  "project_form-raw-scrolled",
			Build: goldenReaderBuild,
			Keys:  []string{"M", "pgdown", "j", "j", "j", "j"},
		},
	}
}

// FixtureScenarios returns every recorded state for the project overview and
// reader.
func FixtureScenarios() []screenfixture.Scenario {
	out := make([]screenfixture.Scenario, 0, 5)
	out = append(out, overviewFixtureScenarios()...)
	out = append(out, formFixtureScenarios()...)
	return out
}

// FixtureScenariosFor returns the scenarios for one screen ID, or nil when the
// ID is not owned by this package.
func FixtureScenariosFor(id screenhost.ID) []screenfixture.Scenario {
	switch id {
	case screenhost.Project:
		return overviewFixtureScenarios()
	case screenhost.ProjectForm:
		return formFixtureScenarios()
	default:
		return nil
	}
}
