package agent

import (
	"strings"
	"testing"

	"omakiten/internal/config"
)

// snapshotWithAgentOutputLanguage builds a snapshot whose
// AgentOutputLanguage returns the supplied directive. The rest of the
// canonical agent test bundle is preserved so test paths that also
// exercise workflow / task / project plumbing keep working.
func snapshotWithAgentOutputLanguage(t *testing.T, value string) *config.Snapshot {
	t.Helper()
	bundle := agentTestBundle(t)
	bundle.Languages = []config.Language{{Code: "en", Name: "English", Native: "English"}}
	bundle.Config.Languages = config.LanguageSettings{AgentOutput: value}
	return config.BuildSnapshot(bundle)
}

func TestResolveCommandAppendsOutputLanguageWhenConfigured(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetSnapshot(snapshotWithAgentOutputLanguage(t, "English"))

	resp, err := fixture.service.ResolveCommand(fixture.ctx, ResolveCommandInput{Name: "okt"})
	if err != nil {
		t.Fatalf("ResolveCommand: %v", err)
	}
	if resp.AgentOutputLanguage != "English" {
		t.Fatalf("response field: got %q, want %q", resp.AgentOutputLanguage, "English")
	}
	if !strings.HasSuffix(strings.TrimRight(resp.Markdown, "\n"), "**Output language:** English") {
		t.Fatalf("Markdown should end with trailing directive, got:\n%s", resp.Markdown)
	}
}

func TestResolveCommandOmitsOutputLanguageWhenEmpty(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetSnapshot(snapshotWithAgentOutputLanguage(t, ""))

	resp, err := fixture.service.ResolveCommand(fixture.ctx, ResolveCommandInput{Name: "okt"})
	if err != nil {
		t.Fatalf("ResolveCommand: %v", err)
	}
	if resp.AgentOutputLanguage != "" {
		t.Fatalf("response field: got %q, want empty", resp.AgentOutputLanguage)
	}
	if strings.Contains(resp.Markdown, "**Output language:**") {
		t.Fatalf("Markdown should not contain output language directive when empty, got:\n%s", resp.Markdown)
	}
}

func TestResolveCommandOutputLanguageWhitespaceOnlyIsTreatedAsEmpty(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetSnapshot(snapshotWithAgentOutputLanguage(t, "   "))

	resp, err := fixture.service.ResolveCommand(fixture.ctx, ResolveCommandInput{Name: "okt"})
	if err != nil {
		t.Fatalf("ResolveCommand: %v", err)
	}
	if strings.Contains(resp.Markdown, "**Output language:**") {
		t.Fatalf("whitespace-only AgentOutputLanguage should be skipped, got:\n%s", resp.Markdown)
	}
}

func TestResolveCommandOutputLanguageAcceptsFreeFormValue(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetSnapshot(snapshotWithAgentOutputLanguage(t, "Português (Brasil)"))

	resp, err := fixture.service.ResolveCommand(fixture.ctx, ResolveCommandInput{Name: "okt"})
	if err != nil {
		t.Fatalf("ResolveCommand: %v", err)
	}
	if !strings.Contains(resp.Markdown, "**Output language:** Português (Brasil)") {
		t.Fatalf("free-form directive should pass through verbatim, got:\n%s", resp.Markdown)
	}
}

func TestResolveCommandOutputLanguageBeforeWithoutCatalogStaysEmpty(t *testing.T) {
	// Without a snapshot installed (degraded path), the renderer must not
	// emit a directive and the response field stays empty.
	fixture := newAgentFixture(t)
	resp, err := fixture.service.ResolveCommand(fixture.ctx, ResolveCommandInput{Name: "okt"})
	if err != nil {
		t.Fatalf("ResolveCommand: %v", err)
	}
	if resp.AgentOutputLanguage != "" {
		t.Fatalf("unwired snapshot should leave directive empty, got %q", resp.AgentOutputLanguage)
	}
	if strings.Contains(resp.Markdown, "**Output language:**") {
		t.Fatalf("unwired snapshot should not emit directive, got:\n%s", resp.Markdown)
	}
}
