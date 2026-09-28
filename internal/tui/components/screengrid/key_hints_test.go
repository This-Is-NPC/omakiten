package screengrid

import (
	"reflect"
	"strings"
	"testing"

	"omakiten/internal/tui/components/screenlayout"
)

func TestRootCellKeyHintsMatchTheSingleZoneFooterVocabulary(t *testing.T) {
	spec := screenlayout.Spec{ID: "body", Scroll: screenlayout.ScrollItems}
	root := Cell(spec, nil)

	got := KeyHints(root)
	want := screenlayout.KeyHints(screenlayout.Func{Def: spec})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("root Cell footer bindings differ from the single-zone vocabulary:\n got %#v\nwant %#v", got, want)
	}

	for _, wantFooter := range []string{"j/k", "pgup/pgdn"} {
		found := false
		for _, binding := range got {
			if binding.Footer == wantFooter || strings.Contains(binding.Footer, wantFooter) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("root Cell footer omitted scroll hint %q", wantFooter)
		}
	}
}
