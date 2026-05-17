package agent

import (
	"testing"
)

func TestContinueTaskSurfacesAgentOutputLanguage(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetSnapshot(snapshotWithAgentOutputLanguage(t, "Português (Brasil)"))

	resp, err := fixture.service.ContinueTask(fixture.ctx, ContinueTaskInput{TaskID: fixture.taskA1.ID})
	if err != nil {
		t.Fatalf("ContinueTask: %v", err)
	}
	if resp.AgentOutputLanguage != "Português (Brasil)" {
		t.Fatalf("ContinueTask agent_output_language: got %q, want %q", resp.AgentOutputLanguage, "Português (Brasil)")
	}
}

func TestContinueTaskOmitsAgentOutputLanguageWhenEmpty(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetSnapshot(snapshotWithAgentOutputLanguage(t, ""))

	resp, err := fixture.service.ContinueTask(fixture.ctx, ContinueTaskInput{TaskID: fixture.taskA1.ID})
	if err != nil {
		t.Fatalf("ContinueTask: %v", err)
	}
	if resp.AgentOutputLanguage != "" {
		t.Fatalf("ContinueTask agent_output_language: got %q, want empty", resp.AgentOutputLanguage)
	}
}
