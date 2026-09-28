package taskdetail

import (
	"fmt"
	"strings"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/taskprojection"
	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screenhost"
)

// Task Detail is the widest surface in the screen tree: a detail form, a
// sub-task board and an activity feed that pack side by side above 80 columns
// and cascade into a single focused pane below it. The baseline below records
// the states a layout migration would break — one per structural shape the
// screen can take — at the three recorded geometries.
//
// The fixture data is chosen so the two packings are actually different: at 80
// columns the left column cannot make its 60-cell floor, so the grid stacks
// and a focused pane renders alone when the zone floors do not fit; at 120
// and 200 the same payload paints all three panels at once. Every value long enough to wrap does
// so at 80 and not at 200 — the title, the tag row, the blocker references, the
// comment bodies — so the fixtures pin the wrap column rather than a body that
// happens to fit everywhere.
//
// DETERMINISM. Nothing on this screen reads a clock: every timestamp it prints
// comes from domain.Event.CreatedAt, which is a string on the wire, so the
// fixtures pin literal stamps that will read the same tomorrow. The maps the
// render path walks (blockersForTask's id index) are only ever indexed by
// ordered projection data, never ranged over, and the recorder's double-paint
// proves it.

// goldenNow is the pinned wall clock of the recorded session. Every timestamp
// in the fixture is a literal derived from it, never a formatted time.Now():
// the whole point of a characterization fixture is that it says the same thing
// tomorrow as it does today.
const goldenNow = "2026-04-11"

// goldenTaskID is the task the detail screen is opened on.
const goldenTaskID int64 = 2421

// goldenPriority* are the configured priority ids the fixture uses.
// domain.Priority is an id into a host-owned registry, not an enum, so the
// fixture pins the ids and PriorityLabel below pins their labels — together
// they keep the PRIORITY row and every blocker reference reading the same on
// any machine, whatever priorities the recording host has configured.
const (
	goldenPriorityLow    = domain.Priority(1)
	goldenPriorityNormal = domain.Priority(2)
	goldenPriorityHigh   = domain.Priority(3)
)

// goldenBuckets is the workflow the sub-task board lanes come from. Four lanes
// against the board's ~27-cell lane budget is what makes the lane window a
// window: two lanes fit at 80 and 120, three at 200, so the "showing 1-N of 4"
// hint is part of what the geometries record.
func goldenBuckets() []domain.Bucket {
	return []domain.Bucket{
		{Key: "backlog", Name: "Backlog", Position: 1},
		{Key: "dev", Name: "Development", Position: 2},
		{Key: "review", Name: "Review", Position: 3},
		{Key: "done", Name: "Done", Position: 4},
	}
}

// goldenDescription is the task body. It is long enough to overflow the
// details ceiling (so the arranger's "▼ N below" hint is on screen at every
// width) and its paragraphs are long enough to wrap inside the value column
// at every width, at a different column each time.
func goldenDescription() string {
	return strings.Join([]string{
		"The detail pane has to survive a body whose first paragraph runs well past the value column at every recorded width, because the wrap column is the first thing a layout migration moves.",
		"",
		"- the description is capped at five rendered lines and the remainder is announced, so the cap is what the fixture pins",
		"- pressing f fullscreens the focused section; enter on details opens the Task Description reader",
		"",
		"Everything below the cap exists to prove the cap: if a migration widens the value column the hint's count moves with it.",
		"More prose, so the count is comfortably above zero at 200 columns as well as at 80.",
		"A final paragraph, kept short.",
	}, "\n")
}

// goldenTasks is the task graph the screen resolves against: two ancestors
// above the opened task (so the kicker carries a breadcrumb), three blocker
// targets, and eight sub-tasks spread unevenly over the four lanes so no lane
// is empty by accident and one lane is.
func goldenTasks() []domain.Task {
	parent := int64(2400)
	root := int64(2300)
	child := goldenTaskID
	return []domain.Task{
		{ID: root, Title: "Architecture review closeout", BucketKey: "review", Priority: goldenPriorityHigh},
		{ID: parent, Title: "Characterization goldens for every uncovered screen package", BucketKey: "dev", Priority: goldenPriorityHigh, ParentID: &root},
		{
			ID: goldenTaskID, ParentID: &parent, BucketKey: "dev", Priority: goldenPriorityHigh,
			Title:       "Record three-width characterization goldens for the Task Detail screen",
			Description: goldenDescription(),
		},
		{ID: 2404, Title: "Ship the shared screentest recorder every package records through", BucketKey: "done", Priority: goldenPriorityNormal},
		{ID: 2405, Title: "Pin the 80-column floor as a recorded geometry rather than a review habit", BucketKey: "review", Priority: goldenPriorityHigh},
		{ID: 2406, Title: "Retire the per-package golden harnesses", BucketKey: "dev", Priority: goldenPriorityLow},

		{ID: 2431, Title: "Draft the fixture payload", BucketKey: "backlog", Priority: goldenPriorityNormal, ParentID: &child},
		{ID: 2432, Title: "Record the entry state with every optional section present", BucketKey: "dev", Priority: goldenPriorityHigh, ParentID: &child},
		{ID: 2433, Title: "Record the activity feed scrolled and focused", BucketKey: "dev", Priority: goldenPriorityNormal, ParentID: &child},
		{ID: 2434, Title: "Record the sub-task board on a lane other than the first", BucketKey: "dev", Priority: goldenPriorityLow, ParentID: &child},
		{ID: 2435, Title: "Record the comment editor open over the feed", BucketKey: "review", Priority: goldenPriorityNormal, ParentID: &child},
		{ID: 2436, Title: "Record the blocker picker with a candidate checked", BucketKey: "review", Priority: goldenPriorityHigh, ParentID: &child},
		{ID: 2437, Title: "Verify the fixtures hold no value that changes tomorrow", BucketKey: "done", Priority: goldenPriorityNormal, ParentID: &child},
		{ID: 2438, Title: "Archived attempt kept for the record", BucketKey: "done", Priority: goldenPriorityLow, ParentID: &child, State: domain.TaskStateArchived},
	}
}

// goldenDependencies are the blockers the detail form lists. Three of them, so
// the KICKER count is not 1 and the block is tall enough to matter to the
// left-column budget.
func goldenDependencies() []domain.TaskDependency {
	return []domain.TaskDependency{
		{TaskID: goldenTaskID, DependsOnTaskID: 2404},
		{TaskID: goldenTaskID, DependsOnTaskID: 2405},
		{TaskID: goldenTaskID, DependsOnTaskID: 2406},
	}
}

// goldenActivity is the feed. It mixes every card the activity column knows how
// to paint — comments with and without tags, a comment whose body overflows the
// six-line card cap, an empty-bodied comment, and one system card per event
// label branch including the unlabelled default — and is long enough to
// overflow the feed viewport at 200x50, so a scrolled recording is scrolled at
// every geometry rather than only at the floor.
func goldenActivity() []domain.Event {
	stamp := func(clock string) string { return goldenNow + " " + clock }
	tag := func(id int64, label string) domain.Tag {
		return domain.Tag{ID: id, Name: label, Label: label}
	}
	return []domain.Event{
		{ID: 9001, EventType: domain.EventTypeTaskCreated, CreatedAt: stamp("08:02"), Payload: `{"bucket":"backlog"}`},
		{ID: 9002, EventType: domain.EventTypeComment, AuthorType: "user", CreatedAt: stamp("08:14"), Body: "Opening the surface for characterization. The activity column is the half of this screen a layout change is most likely to move, so it gets the longest bodies in the fixture."},
		{ID: 9003, EventType: domain.EventTypeTaskMoved, CreatedAt: stamp("08:20"), Payload: `{"from":"backlog","to":"dev"}`},
		{ID: 9004, EventType: domain.EventTypeComment, AuthorType: "agent", CreatedAt: stamp("09:05"), Body: "Recorder wired.\nThe three geometries are the 80-column floor, the 120x40 default and a wide terminal.\nEach one packs the three panels differently.", Tags: []domain.Tag{tag(1, "plan"), tag(2, "tui")}},
		{ID: 9005, EventType: domain.EventTypeComment, AuthorType: "user", CreatedAt: stamp("09:31"), Body: strings.Join([]string{
			"This body is deliberately taller than the six-line card cap so the",
			"card's own overflow hint is part of the baseline.",
			"Line three.",
			"Line four.",
			"Line five.",
			"Line six.",
			"Line seven, which the cap hides behind the hint.",
			"Line eight, likewise.",
		}, "\n")},
		{ID: 9006, EventType: domain.EventTypeComment, AuthorType: "agent", CreatedAt: stamp("10:02"), Body: "", Tags: []domain.Tag{tag(3, "tests-passing")}},
		{ID: 9007, EventType: domain.EventTypeTaskMoved, CreatedAt: stamp("10:40"), Payload: `{"to":"review"}`},
		{ID: 9008, EventType: domain.EventTypeComment, AuthorType: "user", CreatedAt: stamp("11:12"), Body: "Blockers attached: the shared recorder, the 80-column floor decision and the harness retirement all have to land before this one closes.", Tags: []domain.Tag{tag(4, "blocked"), tag(5, "review"), tag(6, "architecture")}},
		{ID: 9009, EventType: "guard.blocked", CreatedAt: stamp("11:20")},
		{ID: 9010, EventType: domain.EventTypeComment, AuthorType: "agent", CreatedAt: stamp("12:00"), Body: "Determinism pass: no clock reading, no absolute path and no map iteration reaches a rendered byte."},
		{ID: 9011, EventType: domain.EventTypeTaskMoved, CreatedAt: stamp("13:45"), Payload: `{"from":"review","to":"dev"}`},
		{ID: 9012, EventType: domain.EventTypeComment, AuthorType: "user", CreatedAt: stamp("14:18"), Body: "Reopened for the wide-terminal recording; the feed has to overflow at 200x50 too or the scrolled fixture is only scrolled at the floor.", Tags: []domain.Tag{tag(7, "resume")}},
		{ID: 9013, EventType: domain.EventTypeComment, AuthorType: "agent", CreatedAt: stamp("15:02"), Body: "Fixtures written for all six recordings at all three geometries."},
		{ID: 9014, EventType: domain.EventTypeTaskCompleted, CreatedAt: stamp("15:30"), Payload: `{"bucket":"done"}`},
	}
}

// goldenTags are the tags the detail form's TAGS row joins. Seven labels, long
// enough that the row wraps inside the value column at every recorded width and
// therefore records the wrap rather than a single line.
func goldenTags(taskID int64) []domain.Tag {
	if taskID != goldenTaskID {
		return nil
	}
	labels := []string{"tui", "characterization", "goldens", "layout-migration", "screentest", "task-detail", "architecture"}
	tags := make([]domain.Tag, len(labels))
	for i, label := range labels {
		tags[i] = domain.Tag{ID: int64(i + 1), Name: label, Label: label}
	}
	return tags
}

// goldenPriorities is the priority table the host hands over. It replaced a
// pinned label resolver and a pinned badge painter: the screen resolves both
// from this now, so the fixture supplies exactly what the app supplies.
func goldenPriorities() []config.PriorityDefinition {
	return []config.PriorityDefinition{
		{ID: int(goldenPriorityLow), Value: "low", Color: "info"},
		{ID: int(goldenPriorityNormal), Value: "normal", Color: "success"},
		{ID: int(goldenPriorityHigh), Value: "high", Color: "error"},
	}
}

func goldenComments() []domain.Comment {
	comments := make([]domain.Comment, 8)
	for i := range comments {
		comments[i] = domain.Comment{ID: int64(i + 1), TaskID: goldenTaskID}
	}
	return comments
}

// goldenPayload is the whole host-supplied payload, materialised fresh on every
// call so two recordings never share a slice.
func goldenPayload() Payload {
	tasks := goldenTasks()
	return Payload{
		Generation: 12,
		Task:       tasks[2],
		Projection: taskprojection.Build(taskprojection.Input{
			Tasks: tasks, Workflow: domain.Workflow{Buckets: goldenBuckets()},
			Dependencies: goldenDependencies(), Comments: goldenComments(), Priorities: goldenPriorities()}),
		Tasks:        tasks,
		Workflow:     domain.Workflow{Buckets: goldenBuckets()},
		Dependencies: goldenDependencies(),
		Activity:     goldenActivity(),
		Stack:        []int64{2300, 2400},
	}
}

// goldenDeps is the host-owned dependency set, all of it deterministic. Body
// markdown paints through components/markdown with TrueColor forced — there is
// no RenderBody stand-in — so the fixture records the real glamour structure.
func goldenDeps() Deps {
	return Deps{
		Priorities: goldenPriorities(),
		TaskTags:   goldenTags,
		MovePrompt: func(task domain.Task) string {
			return fmt.Sprintf("move #%d to bucket", task.ID)
		},
	}
}

// goldenBuild materialises the screen the way the host does — fresh payload,
// fresh deps, and the LifecycleEnter the host issues before the first paint —
// on every call, because the recorder builds it twice and compares the bytes.
func goldenBuild(frame screenhost.Frame) screenhost.Screen {
	return goldenBuildWith(frame, goldenPayload())
}

func goldenBuildWith(frame screenhost.Frame, payload Payload) screenhost.Screen {
	return screenfixture.Enter(New().Bind(goldenDeps()).Open(payload, frame), frame)
}

// goldenQuietPayload is the full fixture with all but the first activity event
// dropped. It exists for the comment-editor recording alone: the editor is
// appended BELOW the feed inside the activity box and then truncated to the box
// height (renderActivity), and entering comment mode never re-syncs the feed's
// own viewport, so on a feed of any realistic length the editor is clipped away
// entirely and `c` paints a byte-identical screen. One event is the longest
// feed that still leaves the whole editor — border, hints and typed draft — on
// screen at the 80-column floor; see the note on the recording below.
func goldenQuietPayload() Payload {
	payload := goldenPayload()
	payload.Activity = payload.Activity[:1]
	return payload
}

func goldenCommentEditorBuild(frame screenhost.Frame) screenhost.Screen {
	return goldenBuildWith(frame, goldenQuietPayload())
}

// goldenBind re-applies the host-owned deps between messages, exactly as the
// host re-binds. Task Detail keeps its deps on the value, so a rebuilt screen
// that skipped this would silently paint raw priorities and no tags.
func goldenBind(screen screenhost.Screen) screenhost.Screen {
	return screen.(Screen).Bind(goldenDeps())
}

// FixtureScenarios returns every recorded state for the Task Detail screen.
func FixtureScenarios() []screenfixture.Scenario {
	return []screenfixture.Scenario{
		{
			// The entry packing with every optional section on screen at once:
			// the breadcrumbed kicker, the wrapped title, the tag row, the
			// three blocker references, the capped description, the four-lane
			// sub-task board and the activity feed. `J` three times walks the
			// activity cursor without moving focus off the detail pane — the
			// one state that proves the two are independent, and the reason
			// this recording is not just "whatever the screen paints first".
			Name:  "detail-full",
			Build: goldenBuild,
			Bind:  goldenBind,
			Keys:  []string{"J", "J", "J"},
		},
		{
			// Focus on the sub-task board, parked on the third lane and the
			// second card in it. At the 80×24 floor the body stacks and the
			// zone floors do not fit, so the focused board fills the body —
			// the same rule Studio uses. Above 80 the board is the bottom of
			// the left column beside the feed, so the same keys record two
			// genuinely different pictures. The lane cursor is off zero on
			// both axes, which is what pins the lane window.
			Name:  "subtasks-focused",
			Build: goldenBuild,
			Bind:  goldenBind,
			Keys:  []string{"s", "l", "l", "j"},
		},
		{
			// The activity feed focused and scrolled to its end: one tab moves
			// focus off the detail pane onto the feed, `G` parks the cursor
			// on the last event and pgdown pushes the window past it. Scroll and
			// cursor are decoupled on this feed (pgdown moves the window and
			// leaves the cursor), so recording both off zero is what pins which
			// of the two the renderer follows.
			Name:  "activity-scrolled",
			Build: goldenBuild,
			Bind:  goldenBind,
			Keys:  []string{"tab", "G", "pgdown"},
		},
		{
			// The comment editor open over the feed with a draft already typed.
			// It is the one mode that renders INSIDE the activity column rather
			// than replacing the screen, so what this records is the feed
			// shortened by an editor — the state a budget change breaks first.
			//
			// It is the ONE recording that does not use the full fixture feed.
			// The editor is appended below the feed and then clipped to the
			// activity box, and entering comment mode does not shrink the feed
			// to make room, so with the fixture's fourteen events `c` paints
			// nothing at all at any recorded geometry. Recording that would
			// freeze a defect and produce three fixtures byte-identical to
			// detail-full; recording a one-event feed instead — the longest the
			// editor survives whole at the 80-column floor — pins the editor's
			// own layout, which is what the migration has to preserve. Focus is
			// moved onto the feed first because below 80 columns the stacked
			// cascade only paints the focused pane, and the editor lives in the
			// activity one.
			Name:  "comment-editor",
			Build: goldenCommentEditorBuild,
			Bind:  goldenBind,
			Keys:  []string{"tab", "c", "wrapping", " ", "the", " ", "editor", " ", "into", " ", "the", " ", "feed"},
		},
		{
			// The blocker picker: a full-screen mode that replaces the two-column
			// body entirely, opened with the three existing blockers already
			// checked from the payload's dependencies, driven five rows down
			// past them and checking a fourth candidate the fixture did not open
			// with. Both halves of the picker's state are off their defaults —
			// the cursor and the check set — and at the 80-column floor the
			// window is short enough that the "▼ N below" hint is on screen and
			// at 200 it is not.
			//
			// The cursor deliberately stops inside the first ten rows. The
			// blocker Cell owns both the cursor and its item window, so the
			// selected candidate remains painted while the 80-column frame
			// reserves its scroll hints.
			Name:  "blockers-picker",
			Build: goldenBuild,
			Bind:  goldenBind,
			Keys:  []string{"b", "down", "down", "down", "down", "down", " "},
		},
		{
			// The move prompt, opened from the sub-task board so it targets the
			// focused CHILD rather than the task the screen is on — the one
			// state that proves the prompt reads the board's cursor. It renders
			// above an otherwise-normal body, so this fixture also pins the row
			// the prompt costs the panels below it.
			Name:  "move-subtask",
			Build: goldenBuild,
			Bind:  goldenBind,
			Keys:  []string{"s", "l", "j", "m", "review"},
		},
	}
}
