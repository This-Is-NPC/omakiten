package sqlutil

import (
	"testing"
)

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
