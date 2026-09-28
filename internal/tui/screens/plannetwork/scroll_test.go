package plannetwork

import (
	"testing"

	"omakiten/internal/domain"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

// TestPlanNetworkScrollIsItemIndexNotLineOffset pins the scroll unit after the
// screenlayout migration: the arranger owns the offset as an ITEM index, and
// wave↔task separators make item heights vary. Opens the golden outline fixture
// so outlineBlock and the arranger see the same rows, walks the cursor down the
// whole outline THROUGH THE KEY PATH — `g` to the top, then one `j` per row, the
// same spellings the grid now owns — and asserts after every step that Offset
// stays in range and never sits past the cursor.
//
// Driving the keys rather than writing the cursor is what keeps the offset half
// of this test load-bearing: the offset is settled by the keystroke, out of the
// frame that keystroke measured, and by nothing else.
func TestPlanNetworkScrollIsItemIndexNotLineOffset(t *testing.T) {
	frame := screentest.FrameAt(t, 100, 24)
	screen := New().Open(planNetworkGoldenPayload())
	screen = screen.bindFrame(frame)
	rows := screen.planNetworkBuildRows()
	if len(rows) < 10 {
		t.Fatalf("golden outline projected %d rows, want a tall outline", len(rows))
	}
	screen = planNetworkDrive(t, screen, frame, "g")
	for step := 0; step < len(rows); step++ {
		layout := screen.grid.Layout()
		scroll := layout.Offset(sectionOutline)
		cursor := layout.Cursor(sectionOutline)
		if scroll < 0 || scroll >= len(rows) {
			t.Fatalf("step %d: layout.Offset=%d out of row-index range [0,%d)", step, scroll, len(rows))
		}
		if cursor != step {
			t.Fatalf("step %d: layout.Cursor=%d, want %d (cursor mirror broken)", step, cursor, step)
		}
		if cursor < scroll {
			t.Fatalf("step %d: cursor=%d above scroll=%d (cursor scrolled off the top)", step, cursor, scroll)
		}
		screen = planNetworkDrive(t, screen, frame, "j")
	}
}

// TestPlanNetworkScrollResetsWhenRowsEmpty pins the empty-outline path: a plan
// with no waves parks the cursor at the no-selection sentinel and the offset
// at zero.
func TestPlanNetworkScrollResetsWhenRowsEmpty(t *testing.T) {
	frame := screentest.FrameAt(t, 100, 24)
	payload := Payload{Show: domain.PlanShow{Plan: planNetworkGoldenPlan()}, NextClaimableID: 0}
	payload.Show.Plan.Slug = planNetworkGoldenEmptySlug
	screen := New().Open(payload).bindFrame(frame)
	screen.syncPlanNetworkScroll(screen.planNetworkBuildRows())
	if got := screen.grid.Layout().Offset(sectionOutline); got != 0 {
		t.Fatalf("empty outline Offset=%d, want 0", got)
	}
	if got := screen.grid.Layout().Cursor(sectionOutline); got != -1 {
		t.Fatalf("empty outline Cursor=%d, want -1", got)
	}
}

func TestPlanNetworkMountsOutlineAsOneGridCell(t *testing.T) {
	frame := screentest.FrameAt(t, 100, 24)
	screen := New().Open(planNetworkGoldenPayload()).bindFrame(frame)
	screen.syncPlanNetworkScroll(screen.planNetworkBuildRows())

	result := screen.gridResult(frame)
	placement, ok := result.Placement(sectionOutline)
	if !ok || !placement.Leaf {
		t.Fatalf("outline placement = %+v, found=%v; want one Cell leaf", placement, ok)
	}
	if got, want := placement.Box, screen.outlineBox(); got != want {
		t.Fatalf("outline box = %+v, want %+v", got, want)
	}
}

func planNetworkDrive(t *testing.T, screen Screen, frame screenhost.Frame, key string) Screen {
	t.Helper()
	next, ok := screen.Update(frame, screentest.Key(key)).Screen.(Screen)
	if !ok {
		t.Fatalf("Update(%q) did not carry a plannetwork Screen", key)
	}
	return next
}
