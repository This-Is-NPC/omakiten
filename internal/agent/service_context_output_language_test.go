package agent

import (
	"testing"
)

func TestDumpContextSurfacesAgentOutputLanguage(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetSnapshot(snapshotWithAgentOutputLanguage(t, "Português (Brasil)"))

	resp, err := fixture.service.DumpContext(fixture.ctx, DumpContextInput{
		ProjectSelector: ProjectSelector{ProjectID: fixture.projectA.ID},
		Level:           1,
	})
	if err != nil {
		t.Fatalf("DumpContext: %v", err)
	}
	if resp.AgentOutputLanguage != "Português (Brasil)" {
		t.Fatalf("DumpContext agent_output_language: got %q, want %q", resp.AgentOutputLanguage, "Português (Brasil)")
	}
}

func TestDumpContextOmitsAgentOutputLanguageWhenEmpty(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetSnapshot(snapshotWithAgentOutputLanguage(t, ""))

	resp, err := fixture.service.DumpContext(fixture.ctx, DumpContextInput{
		ProjectSelector: ProjectSelector{ProjectID: fixture.projectA.ID},
		Level:           1,
	})
	if err != nil {
		t.Fatalf("DumpContext: %v", err)
	}
	if resp.AgentOutputLanguage != "" {
		t.Fatalf("DumpContext agent_output_language: got %q, want empty", resp.AgentOutputLanguage)
	}
}
