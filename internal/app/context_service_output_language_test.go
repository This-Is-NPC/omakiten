package app

import (
	"context"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/testfixtures"
	"omakiten/internal/token"
)

func TestContextDumpSurfacesAgentOutputLanguage(t *testing.T) {
	ctx := context.Background()
	bundle := appTestBundle(t, 1000)
	bundle.Languages = []config.Language{{Code: "en", Name: "English", Native: "English"}}
	bundle.Config.Languages = config.LanguageSettings{AgentOutput: "English"}
	store, project := appTestStore(t, bundle)
	defer func() { _ = store.Close() }()

	service := NewContextService(store, store, store, store, store.Snapshot(), token.ApproxCounter{}, testfixtures.CanonicalRegistry())
	dump, err := service.Dump(ctx, project.Context(), 1)
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	if dump.AgentOutputLanguage != "English" {
		t.Fatalf("Dump agent_output_language: got %q, want %q", dump.AgentOutputLanguage, "English")
	}
}

func TestContextDumpOmitsAgentOutputLanguageWhenEmpty(t *testing.T) {
	ctx := context.Background()
	bundle := appTestBundle(t, 1000)
	store, project := appTestStore(t, bundle)
	defer func() { _ = store.Close() }()

	service := NewContextService(store, store, store, store, store.Snapshot(), token.ApproxCounter{}, testfixtures.CanonicalRegistry())
	dump, err := service.Dump(ctx, project.Context(), 1)
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	if dump.AgentOutputLanguage != "" {
		t.Fatalf("Dump agent_output_language: got %q, want empty", dump.AgentOutputLanguage)
	}
}
