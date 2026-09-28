package logs

import (
	"testing"

	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/screens/screentest"
)

// TestSummaryDropsBeforeFeedAtRecordedGeometries pins the product drop order:
// summary is declared last with a measured floor, so when eventsMinRows plus
// that floor exceed panelBox the arranger drops summary and keeps the feed.
//
// Reason: the inspector exists to show the event feed; summary aggregates are
// supplemental. Reordering sections or lowering eventsMinRows without
// re-measuring against HostBox would silently reintroduce a crushed feed.
func TestSummaryDropsBeforeFeedAtRecordedGeometries(t *testing.T) {
	t.Parallel()
	for _, geometry := range screentest.Geometries {
		geometry := geometry
		t.Run(geometry.Name, func(t *testing.T) {
			t.Parallel()
			frame := screentest.FrameAt(t, geometry.Width, geometry.Height)
			base, _ := testScreen(t, geometry.Width)
			screen := base.Apply(Payload{Rows: sampleRows(40)})
			kit := frame.Kit()
			res := screengrid.Render(kit, screen.grid, screen.panelBox(kit), screen.bodyRoot(kit))
			events, ok := res.Placement(sectionEvents)
			if !ok || events.Dropped {
				t.Fatalf("events section dropped at %s; the feed is the subject of this screen", geometry.Name)
			}
			boxRows := screen.panelBox(kit).Rows
			floor := eventsMinRows + screen.summaryCell(kit).Spec.MinRows
			summary, hasSummary := res.Placement(sectionSummary)
			kept := hasSummary && !summary.Dropped
			if boxRows < floor && kept {
				t.Fatalf("summary kept at %s (events=%d summary=%d panelBox=%d floor=%d); the supplemental zone must yield when the two floors cannot fit",
					geometry.Name, events.Box.Rows, summary.Box.Rows, boxRows, floor)
			}
			if boxRows >= floor && !kept {
				t.Fatalf("summary dropped at %s (panelBox=%d floor=%d); the supplemental zone must stay when both floors fit",
					geometry.Name, boxRows, floor)
			}
		})
	}
}
