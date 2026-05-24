package tui

import (
	"strings"
	"testing"
)

// TestCardHeightFromSpecMatchesRenderedHeight pins the perf invariant
// that powers syncFocusedColumnScroll: cardHeightFromSpec must report
// the same row count as renderTaskCard's actual output so the board
// scroll math stays honest after we stopped rendering each card just
// to measure it.
func TestCardHeightFromSpecMatchesRenderedHeight(t *testing.T) {
	model := buildRefreshHotPathModel(t)

	cases := []struct {
		name string
		spec taskCardSpec
	}{
		{
			name: "short title no badges no extras",
			spec: taskCardSpec{ID: 1, Title: "Refactor parser", BoxWidth: 26, InnerWidth: 22},
		},
		{
			name: "long title that wraps two ways",
			spec: taskCardSpec{ID: 42, Title: "Investigate the intermittent flaky test on the activity feed pipeline", BoxWidth: 26, InnerWidth: 22},
		},
		{
			name: "narrow width forces aggressive wrap",
			spec: taskCardSpec{ID: 7, Title: "Triage triage triage triage", BoxWidth: 18, InnerWidth: 14},
		},
		{
			name: "extra lines bump height",
			spec: taskCardSpec{ID: 11, Title: "Plan card", ExtraLines: []string{"@alice", "wave 2"}, BoxWidth: 26, InnerWidth: 22},
		},
		{
			name: "single badge",
			spec: taskCardSpec{ID: 99, Title: "T", Badges: []string{"P1"}, BoxWidth: 26, InnerWidth: 22},
		},
		{
			name: "many badges wrap onto multiple rows",
			spec: taskCardSpec{ID: 100, Title: "T", Badges: []string{"P1", "blockers:2", "comments:5", "subtasks:3", "extra-tag"}, BoxWidth: 22, InnerWidth: 18},
		},
		{
			name: "selected adds chevron prefix",
			spec: taskCardSpec{ID: 5, Title: "Selected card", Selected: true, BoxWidth: 26, InnerWidth: 22},
		},
		{
			name: "empty extras are skipped",
			spec: taskCardSpec{ID: 12, Title: "ignore empty", ExtraLines: []string{"", "kept", ""}, BoxWidth: 26, InnerWidth: 22},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := strings.Count(model.renderTaskCard(tc.spec), "\n") + 1
			got := model.cardHeightFromSpec(tc.spec)
			if got != want {
				t.Fatalf("cardHeightFromSpec(%+v) = %d, want %d (renderer)", tc.spec, got, want)
			}
		})
	}
}
