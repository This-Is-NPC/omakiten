package sqlutil

import "testing"

func TestAgentAttributedFilter(t *testing.T) {
	if AgentAttributedFilter != "agent_model != ''" {
		t.Fatalf("AgentAttributedFilter = %q, want agent_model != ''", AgentAttributedFilter)
	}
}

func TestAgentAttributedFilterFor(t *testing.T) {
	tests := []struct {
		alias string
		want  string
	}{
		{"", "agent_model != ''"},
		{"r", "r.agent_model != ''"},
		{"events", "events.agent_model != ''"},
	}
	for _, tt := range tests {
		if got := AgentAttributedFilterFor(tt.alias); got != tt.want {
			t.Fatalf("AgentAttributedFilterFor(%q) = %q, want %q", tt.alias, got, tt.want)
		}
	}
}

func TestConditionalCount(t *testing.T) {
	got := ConditionalCount("event_type = ?")
	want := "SUM(CASE WHEN event_type = ? THEN 1 ELSE 0 END)"
	if got != want {
		t.Fatalf("ConditionalCount = %q, want %q", got, want)
	}
}

func TestConditionalCounts(t *testing.T) {
	got := ConditionalCounts([]string{"a", "b"})
	want := []string{
		"SUM(CASE WHEN a THEN 1 ELSE 0 END)",
		"SUM(CASE WHEN b THEN 1 ELSE 0 END)",
	}
	if len(got) != len(want) {
		t.Fatalf("ConditionalCounts len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ConditionalCounts[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if got := ConditionalCounts(nil); len(got) != 0 {
		t.Fatalf("ConditionalCounts(nil) len = %d, want 0", len(got))
	}
}
