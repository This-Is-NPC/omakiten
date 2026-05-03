package tui

import (
	"testing"

	"omakiten/internal/domain"
	"omakiten/internal/token"
)

// TestComputeMetricsExcludesSkillBodies guards the token-counting policy
// change: law bodies and persona descriptions count toward the budget; skill
// bodies do not.
func TestComputeMetricsExcludesSkillBodies(t *testing.T) {
	model := Model{
		counter: token.ApproxCounter{},
		laws: []domain.Law{
			{Key: "scope", Body: "five tokens of body content"},
		},
		personas: []domain.Persona{
			{Key: "agent", Description: "three persona words"},
		},
		skills: []domain.Skill{
			{Key: "go", Body: "this skill body should not be counted at all"},
		},
	}

	// Expected: law key + body (1 + 5) + persona description (3) = 9.
	got := model.computeMetrics(0)
	if got.EstimatedTotal != 9 {
		t.Fatalf("computeMetrics().EstimatedTotal = %d, want 9 (skill body must be excluded)", got.EstimatedTotal)
	}
	if got.Truncated {
		t.Fatalf("computeMetrics().Truncated = true with maxTokens=0, want false")
	}

	// With a tight budget the metrics should report Truncated.
	tight := model.computeMetrics(2)
	if !tight.Truncated {
		t.Fatalf("computeMetrics(maxTokens=2).Truncated = false, want true")
	}
}
