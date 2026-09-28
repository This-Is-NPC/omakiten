package tui

import (
	"strings"
	"testing"

	"omakiten/internal/tui/components/card"
)

// TestCardHeightMatchesRenderedHeight pins the invariant syncFocusedColumnScroll
// runs on: the height the board budgets against is the height the card renders
// to. The card package proves the two share one line builder; this proves the
// painter the ROOT wires up — real theme, real border, real padding — agrees.
func TestCardHeightMatchesRenderedHeight(t *testing.T) {
	painter := buildRefreshHotPathModel(t).cardPainter()

	cases := []struct {
		name string
		spec card.Spec
	}{
		{
			name: "short title no badges no extras",
			spec: card.Spec{ID: 1, Title: "Refactor parser", BoxWidth: 26, InnerWidth: 22},
		},
		{
			name: "long title that wraps two ways",
			spec: card.Spec{ID: 42, Title: "Investigate the intermittent flaky test on the activity feed pipeline", BoxWidth: 26, InnerWidth: 22},
		},
		{
			name: "narrow width forces aggressive wrap",
			spec: card.Spec{ID: 7, Title: "Triage triage triage triage", BoxWidth: 18, InnerWidth: 14},
		},
		{
			name: "meta rows bump height",
			spec: card.Spec{ID: 11, Title: "Plan card", Meta: []string{"@alice", "wave 2"}, BoxWidth: 26, InnerWidth: 22},
		},
		{
			name: "single badge",
			spec: card.Spec{ID: 99, Title: "T", Badges: []string{"P1"}, BoxWidth: 26, InnerWidth: 22},
		},
		{
			name: "many badges wrap onto multiple rows",
			spec: card.Spec{ID: 100, Title: "T", Badges: []string{"P1", "blockers:2", "comments:5", "subtasks:3", "extra-tag"}, BoxWidth: 22, InnerWidth: 18},
		},
		{
			name: "the cursor chevron costs the title two columns",
			spec: card.Spec{ID: 5, Title: "Selected card", Selected: true, Cursor: true, BoxWidth: 26, InnerWidth: 22},
		},
		{
			name: "empty meta rows are skipped",
			spec: card.Spec{ID: 12, Title: "ignore empty", Meta: []string{"", "kept", ""}, BoxWidth: 26, InnerWidth: 22},
		},
		{
			name: "an unbreakable title is cut, not overflowed",
			spec: card.Spec{ID: 13, Title: strings.Repeat("x", 80), BoxWidth: 26, InnerWidth: 22},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := strings.Count(painter.Render(tc.spec), "\n") + 1
			if got := painter.Height(tc.spec); got != want {
				t.Fatalf("Height(%+v) = %d, want %d (renderer)", tc.spec, got, want)
			}
		})
	}
}
