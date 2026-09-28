package taskdetail

import (
	"fmt"
	"reflect"
	"testing"

	"omakiten/internal/domain"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

// feedFixture parks a screen on the activity zone with a feed deep enough that
// windowing, wrapping and the cursor snap are all live.
//
// It uses screentest's THEMED frame, not testFrame: the focused card differs
// from an unfocused one by its border colour alone, so under the bare frame
// (which carries zero-value styles) every card renders identically and a memo
// that repainted nothing would look correct.
func feedFixture(t *testing.T) (Screen, screenhost.Frame) {
	t.Helper()
	frame := screentest.FrameAt(t, 120, 40)
	screen := New().Open(taskDetailBenchPayload(60, 24), frame)
	for _, k := range []string{"tab"} {
		screen = screen.Update(frame, key(k)).Screen.(Screen)
	}
	if screen.focus != FocusActivity {
		t.Fatalf("fixture did not park on the activity zone, got focus %v", screen.focus)
	}
	return screen, frame
}

// TestActivityFeedMemoNeverServesWhatAFullRenderWouldNot is the guard on the
// only dangerous property a memo has. After every keystroke the feed the screen
// is actually showing must be byte-for-byte the feed a full, uncached render of
// the same state would have produced — in BOTH coordinate spaces, because the
// cards and the ranges that index them are only meaningful together.
//
// A drift here would be silent: the screen would keep painting, just from a
// flattening that no longer matches the cursor.
func TestActivityFeedMemoNeverServesWhatAFullRenderWouldNot(t *testing.T) {
	screen, frame := feedFixture(t)
	keys := []string{"j", "j", "j", "k", "G", "j", "g", "k", "pgdown", "j", "j", "pgup", "k", "tab", "tab", "tab", "j"}
	for round := 0; round < 6; round++ {
		for _, k := range keys {
			screen = screen.Update(frame, key(k)).Screen.(Screen)
			if screen.focus != FocusActivity || !screen.feed.ready {
				continue
			}
			full := screen.renderActivityFeed(screen.activityFeedKey())
			if !reflect.DeepEqual(full.cards, screen.feed.cards) {
				t.Fatalf("round %d key %q: cached cards differ from a full render", round, k)
			}
			// The cards the arranger is HANDED are the memo repainted at its
			// cursor, so the second half of the old two-space check lives here:
			// what is on screen has to equal what a full render would produce.
			if !reflect.DeepEqual(screen.feedCards(screen.activityCursor()), full.cards) {
				t.Fatalf("round %d key %q: the cards handed to the arranger differ from a full render", round, k)
			}
		}
	}
}

// TestCursorMoveRepaintsTwoCardsNotSixty pins the mechanism the speed-up rests
// on, and pins it as a MEASUREMENT rather than as a claim, because the thing
// being asserted is a cost.
//
// A repaint cannot be detected by comparing the rendered cards: under the test
// colour profile the accent border is stripped, so a focused card and an
// unfocused one are byte-identical — which is exactly why acceptance here is an
// allocation ratio and not a diff. A cursor move that still composed all sixty
// cards would land within a few percent of the full render instead of under a
// fifth of it.
func TestCursorMoveRepaintsTwoCardsNotSixty(t *testing.T) {
	screen, _ := feedFixture(t)
	feed := screen.currentActivityFeed()
	moved := screen.withActivityCursorAt(screen.activityCursor() + 1)

	if !reflect.DeepEqual(moved.refocused(feed).cards, moved.renderActivityFeed(moved.activityFeedKey()).cards) {
		t.Fatal("the incremental repaint and a full render disagree on the cards")
	}

	full := testing.AllocsPerRun(20, func() { _ = moved.renderActivityFeed(moved.activityFeedKey()) })
	incremental := testing.AllocsPerRun(20, func() { _ = moved.refocused(feed) })
	if limit := full / 5; incremental > limit {
		t.Fatalf("a cursor move allocated %.0f, want under %.0f — a fifth of the %.0f a full feed render costs",
			incremental, limit, full)
	}
	t.Logf("full feed render %.0f allocs, cursor-move repaint %.0f allocs", full, incremental)
}

// TestKeystrokeRendersTheDetailsBlockOncePerPaint counts the renders of the
// details block, which is the second multiplier this screen carried: the block
// was composed once for its own pixels and up to three more times purely to
// measure its height.
//
// Deps.TaskTags is called exactly once per renderDetails and nowhere else, so
// it is an honest counter. Comment counts now arrive in the shared projection
// and are read by both the detail row and sub-task cards. The remaining renders
// are named:
// After the screenlayout migration (#2425) the details block is a SECTION, so
// the arranger rendered it twice — once inside HandleKey, to resolve the frame
// the key moved within, and once inside the Arrange that paints the frame after.
// Two was the arranger's floor WITHOUT a cross-call memo, and #2448 gave it one:
// a keystroke now carries the measurement of the body it is moving within
// instead of retaking it, so the only render left is the one that paints.
//
// One in every zone is the point. The details block does not read the arranger's
// cursor, so a keystroke anywhere on this screen — including one in the feed,
// which does — never renders it a second time.
func TestKeystrokeRendersTheDetailsBlockOncePerPaint(t *testing.T) {
	cases := []struct {
		name string
		zone []string
		want int
	}{
		{name: "details", want: 1},
		{name: "subtasks", zone: []string{"tab", "tab"}, want: 1},
		{name: "activity", zone: []string{"tab"}, want: 1},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			calls := 0
			frame := testFrame(120, 40)
			screen := New().Bind(Deps{TaskTags: func(int64) []domain.Tag { calls++; return nil }}).
				Open(taskDetailBenchPayload(60, 24), frame)
			for _, k := range testCase.zone {
				screen = screen.Update(frame, key(k)).Screen.(Screen)
			}
			calls = 0
			screen = screen.Update(frame, key("j")).Screen.(Screen)
			_ = screen.View(frame)
			if calls > testCase.want {
				t.Fatalf("one keystroke rendered the details block %d times, want at most %d", calls, testCase.want)
			}
		})
	}
}

// TestActivityFeedMemoIsDroppedWhenItsInputsLeaveTheKey covers the two ways the
// feed's inputs move without the key noticing: a theme or language swap, which
// can only happen across a navigation and therefore across a lifecycle event.
func TestActivityFeedMemoIsDroppedWhenItsInputsLeaveTheKey(t *testing.T) {
	events := []screenhost.LifecycleEvent{screenhost.LifecycleEnter, screenhost.LifecycleFocus, screenhost.LifecycleResize, screenhost.LifecycleLeave}
	for _, event := range events {
		screen, frame := feedFixture(t)
		screen = screen.Update(frame, key("j")).Screen.(Screen)
		if !screen.feed.ready {
			t.Fatalf("%v: fixture did not warm the memo", event)
		}
		after := screen.Lifecycle(frame, event).Screen.(Screen)
		if after.feed.ready {
			t.Fatalf("%v: memo survived a lifecycle event", event)
		}
	}

}

func taskDetailBenchPayload(comments, subtasks int) Payload {
	parent := domain.Task{ID: 10, Title: "Pilot screenlayout on Project and Task Detail", BucketKey: "dev"}
	payload := Payload{
		Generation: 1,
		Task:       parent,
		Tasks:      []domain.Task{parent},
		Workflow: domain.Workflow{Buckets: []domain.Bucket{
			{Key: "backlog", Name: "Backlog", Position: 1},
			{Key: "dev", Name: "Development", Position: 2},
			{Key: "review", Name: "Review", Position: 3},
			{Key: "done", Name: "Done", Position: 4},
		}},
	}
	buckets := []string{"backlog", "dev", "review", "done"}
	for i := 0; i < subtasks; i++ {
		payload.Tasks = append(payload.Tasks, domain.Task{
			ID:        int64(100 + i),
			Title:     fmt.Sprintf("subtask %02d — a title long enough that the card wraps inside its column", i),
			BucketKey: buckets[i%len(buckets)],
			ParentID:  ptr(int64(10)),
		})
	}
	for i := 0; i < comments; i++ {
		payload.Activity = append(payload.Activity, domain.Event{
			ID: int64(1000 + i), EventType: domain.EventTypeComment, AuthorType: "agent",
			CreatedAt: "2026-08-07 10:00:00",
			Body:      fmt.Sprintf("comment %02d — a body of a couple of sentences, long enough to wrap to several rows inside the comment card at every recorded geometry so the feed is a body of multi-line items.", i),
		})
	}
	return payload
}
