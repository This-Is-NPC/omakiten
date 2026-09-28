package screenlayout

import (
	"testing"

	"omakiten/internal/tui/components/screenkit"
)

func TestListAndInspectorBakeTheListInspectorBreakpoint(t *testing.T) {
	t.Parallel()
	list := List("list")
	if list.MinWidth != screenkit.ListFloor || list.MinRows != listInspectorPaneMinRows ||
		list.Weight != listColumnWeight || list.ColumnGap != screenkit.ZoneGap ||
		list.Scroll != ScrollItems || !list.SelectFirst {
		t.Fatalf("List produced %+v", list)
	}
	insp := Inspector("inspector")
	if insp.MinWidth != screenkit.InspectorFloor || insp.MinRows != listInspectorPaneMinRows ||
		insp.Weight != inspectorColumnWeight || insp.ColumnGap != screenkit.ZoneGap ||
		insp.Scroll != ScrollNone {
		t.Fatalf("Inspector produced %+v", insp)
	}
}

func TestInspectorIDsFieldsAndDetailMatchTheLiftedStudioSpecs(t *testing.T) {
	t.Parallel()
	ids := InspectorIDs{Inspector: "inspector", Fields: "fields", Detail: "detail"}

	fields := ids.FieldsSpec(6)
	if fields.ID != "fields" || fields.MinWidth != screenkit.InspectorFloor ||
		fields.MinRows != 5 || fields.MaxRows != 0 || fields.ColumnMaxRows != 21 || fields.Weight != 4 ||
		fields.ColumnGap != screenkit.ZoneGap || fields.Scroll != ScrollItems ||
		fields.SelectFirst {
		t.Fatalf("FieldsSpec(6) produced %+v", fields)
	}
	if got := ids.FieldsSpec(0).ColumnMaxRows; got != 0 {
		t.Fatalf("FieldsSpec(0) ceiling = %d, want 0", got)
	}

	detail := ids.DetailSpec(false, 0)
	if detail.ID != "detail" || detail.MinWidth != screenkit.InspectorFloor ||
		detail.MinRows != 5 || detail.Weight != 1 || detail.ColumnGap != screenkit.ZoneGap ||
		detail.Scroll != ScrollItems || detail.SelectFirst || detail.MaxRows != 0 {
		t.Fatalf("DetailSpec(false, 0) produced %+v", detail)
	}
	related := ids.DetailSpec(true, 1)
	if related.MinRows != 6 || !related.SelectFirst {
		t.Fatalf("DetailSpec(true, 1) produced %+v", related)
	}
}

func TestFeedAndBesideFeedBakeTheStackedFeedShape(t *testing.T) {
	t.Parallel()
	feed := Feed("activity", 3)
	if feed.MinWidth != screenkit.FeedFloor || feed.MaxWidth != screenkit.ComfortableReadingWidth ||
		feed.WidthPercent != screenkit.FeedProportion || feed.MinRows != 3 ||
		feed.ColumnGap != screenkit.ZoneGap || feed.Weight != feedColumnWeight ||
		!feed.Column || feed.Scroll != ScrollItems || feed.SelectFirst {
		t.Fatalf("Feed produced %+v", feed)
	}
	meta := BesideFeed("meta", "left", screenkit.PanelFloor, 3, ScrollItems)
	if meta.MinWidth != screenkit.PanelFloor || meta.MinRows != 3 ||
		meta.Group != "left" || !meta.Column || meta.ColumnGap != screenkit.ZoneGap ||
		meta.Weight != stackedColumnWeight || meta.Scroll != ScrollItems {
		t.Fatalf("BesideFeed panel produced %+v", meta)
	}
	details := BesideFeed("details", "left", screenkit.DetailColumnFloor, 6, ScrollItems)
	if details.MinWidth != screenkit.DetailColumnFloor || details.MinRows != 6 {
		t.Fatalf("BesideFeed details produced %+v", details)
	}
	board := BesideFeed("subtasks", "left", screenkit.DetailColumnFloor, 6, ScrollNone)
	if board.Scroll != ScrollNone {
		t.Fatalf("BesideFeed board Scroll = %v, want ScrollNone", board.Scroll)
	}
}

func TestInspectorFieldsCeilingIsTheLiftedFormula(t *testing.T) {
	t.Parallel()
	if got := inspectorFieldsCeiling(0); got != 0 {
		t.Errorf("ceiling(0) = %d, want 0", got)
	}
	if got := inspectorFieldsCeiling(1); got != 6 {
		t.Errorf("ceiling(1) = %d, want 6", got)
	}
	if got := inspectorFieldsCeiling(9); got != 30 {
		t.Errorf("ceiling(9) = %d, want 30", got)
	}
}
