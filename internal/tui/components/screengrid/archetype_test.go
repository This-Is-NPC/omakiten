package screengrid

import (
	"testing"

	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
)

func emptyBody(screenlayout.Canvas) screenlayout.Block {
	return screenlayout.Block{}
}

func TestListInspectorIsTwoColumnsWithTheVocabularyGap(t *testing.T) {
	t.Parallel()
	ids := screenlayout.InspectorIDs{Inspector: "inspector", Fields: "fields", Detail: "detail"}
	list := Cell(screenlayout.List("list"), emptyBody)
	inspector := InspectorColumn(ids,
		Cell(ids.FieldsSpec(6), emptyBody),
		Cell(ids.DetailSpec(false, 0), emptyBody),
		true, true)
	root := ListInspector("commands", list, inspector)

	if root.kind != kindCols {
		t.Fatalf("ListInspector kind = %d, want Cols", root.kind)
	}
	if root.Spec.ID != "commands" || root.Spec.ColumnGap != screenkit.ZoneGap {
		t.Fatalf("ListInspector spec = %+v", root.Spec)
	}
	if len(root.children) != 2 {
		t.Fatalf("ListInspector children = %d, want 2", len(root.children))
	}
	if got := root.children[0].Spec.MinWidth; got != screenkit.ListFloor {
		t.Errorf("list MinWidth = %d, want ListFloor %d", got, screenkit.ListFloor)
	}
	if got := root.children[1].Spec.MinWidth; got != screenkit.InspectorFloor {
		t.Errorf("inspector MinWidth = %d, want InspectorFloor %d", got, screenkit.InspectorFloor)
	}
	if root.children[1].kind != kindRows || len(root.children[1].children) != 2 {
		t.Fatalf("inspector column = kind %d with %d children, want Rows of two",
			root.children[1].kind, len(root.children[1].children))
	}
}

func TestInspectorColumnOmitsTheZoneTheRowDoesNotHave(t *testing.T) {
	t.Parallel()
	ids := screenlayout.InspectorIDs{Inspector: "inspector", Fields: "fields", Detail: "detail"}
	fields := Cell(ids.FieldsSpec(2), emptyBody)
	detail := Cell(ids.DetailSpec(false, 0), emptyBody)

	both := InspectorColumn(ids, fields, detail, true, true)
	if len(both.children) != 2 || both.children[0].Spec.ID != "fields" || both.children[1].Spec.ID != "detail" {
		t.Fatalf("both: children = %v", idsOf(both.children))
	}
	onlyDetail := InspectorColumn(ids, fields, detail, false, true)
	if len(onlyDetail.children) != 1 || onlyDetail.children[0].Spec.ID != "detail" {
		t.Fatalf("detail only: children = %v", idsOf(onlyDetail.children))
	}
	onlyFields := InspectorColumn(ids, fields, detail, false, false)
	if len(onlyFields.children) != 1 || onlyFields.children[0].Spec.ID != "fields" {
		t.Fatalf("fields only: children = %v", idsOf(onlyFields.children))
	}
}
