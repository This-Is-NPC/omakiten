package agent

import (
	"testing"

	"omakiten/internal/domain"
)

func TestSolutionSummaryEmitsLikesBadge(t *testing.T) {
	cases := []struct {
		name      string
		likes     int
		wantBadge string
	}{
		{name: "zero likes is unbadged", likes: 0, wantBadge: ""},
		{name: "single like", likes: 1, wantBadge: "[★ 1]"},
		{name: "many likes", likes: 7, wantBadge: "[★ 7]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			summary := solutionSummary(domain.Solution{ID: 1, ErrorID: 2, Description: "x", Likes: tc.likes})
			if summary.Likes != tc.likes {
				t.Fatalf("Likes = %d, want %d", summary.Likes, tc.likes)
			}
			if summary.LikesBadge != tc.wantBadge {
				t.Fatalf("LikesBadge = %q, want %q", summary.LikesBadge, tc.wantBadge)
			}
		})
	}
}

func TestSolutionSummaryPropagatesProjectContext(t *testing.T) {
	summary := solutionSummary(domain.Solution{ID: 1, ErrorID: 2, Description: "x", ProjectID: 99, ProjectSlug: "demo"})
	if summary.ProjectID != 99 || summary.ProjectSlug != "demo" {
		t.Fatalf("project context not propagated: %+v", summary)
	}
}
