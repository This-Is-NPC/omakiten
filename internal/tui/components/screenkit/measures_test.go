package screenkit

import (
	"testing"
)

// The census these names were lifted from. A value that moves here is a
// redesign, not a rename: the goldens of every screen that reads the name
// will move with it, and that is a decision, not a refactor.
func TestMeasuresAreTheCensusNotARedesign(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		got  int
		want int
	}{
		{"ComfortableReadingWidth", ComfortableReadingWidth, 96},
		{"FeedFloor", FeedFloor, 44},
		{"FeedProportion", FeedProportion, 45},
		{"ListFloor", ListFloor, 40},
		{"InspectorFloor", InspectorFloor, 40},
		{"PanelFloor", PanelFloor, 32},
		{"DetailColumnFloor", DetailColumnFloor, 60},
		{"ZoneGap", ZoneGap, 2},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want the census %d", tc.name, tc.got, tc.want)
		}
	}
}
