package config

import (
	"testing"
)

// TestBuildSnapshotTrimsLanguageKeysForInactiveCodes pins the RAM-
// reduction contract from task #224: every locale pack that is not
// the active CLI / TUI / agent-output code (and not the en baseline)
// loses its Keys map post-BuildSnapshot. The picker only needs
// Code/Name/Native; the catalogs already hold pointers into the
// active + baseline languages so the trim does not break resolution.
func TestBuildSnapshotTrimsLanguageKeysForInactiveCodes(t *testing.T) {
	bundle := Bundle{
		Workflows: []Workflow{{
			ID: 1, Key: "demo", Name: "Demo",
			Buckets: []Bucket{{ID: 1, Key: "backlog", Name: "Backlog", Position: 1}},
		}},
		Config: Settings{
			Workflow: WorkflowSettings{Active: "demo"},
			Languages: LanguageSettings{CLI: "pt-br", TUI: "en", AgentOutput: ""},
		},
		Languages: []Language{
			{Code: "en", Name: "English", Native: "English", Keys: map[string]string{"k": "english"}},
			{Code: "pt-br", Name: "Portuguese (Brazil)", Native: "Português (Brasil)", Keys: map[string]string{"k": "portuguese"}},
			{Code: "fr", Name: "French", Native: "Français", Keys: map[string]string{"k": "french"}},
			{Code: "de", Name: "German", Native: "Deutsch", Keys: map[string]string{"k": "german"}},
		},
	}
	snap := BuildSnapshot(bundle)

	// Active codes keep their Keys: catalogs resolve them.
	if got := snap.Catalog(SurfaceCLI).Get("k"); got != "portuguese" {
		t.Fatalf("CLI catalog should resolve via pt-br Keys, got %q", got)
	}
	if got := snap.Catalog(SurfaceTUI).Get("k"); got != "english" {
		t.Fatalf("TUI catalog should resolve via en Keys (active TUI + baseline), got %q", got)
	}

	// Inactive codes lose their Keys but keep Code/Name/Native for
	// the picker.
	for _, lang := range snap.Languages() {
		switch lang.Code {
		case "pt-br", "en":
			if len(lang.Keys) == 0 {
				t.Fatalf("active code %q lost its Keys", lang.Code)
			}
		case "fr", "de":
			if lang.Keys != nil {
				t.Fatalf("inactive code %q kept Keys (len=%d) — RAM trim failed", lang.Code, len(lang.Keys))
			}
			if lang.Name == "" || lang.Native == "" {
				t.Fatalf("inactive code %q lost picker fields (Name=%q Native=%q)", lang.Code, lang.Name, lang.Native)
			}
		}
	}

	// LanguageByCode also returns trimmed entries for inactive codes —
	// the picker callsite reads this when surfacing display names.
	fr, ok := snap.LanguageByCode("fr")
	if !ok {
		t.Fatalf("LanguageByCode(fr) missing")
	}
	if fr.Keys != nil {
		t.Fatalf("LanguageByCode(fr) Keys still populated (len=%d)", len(fr.Keys))
	}
	if fr.Native != "Français" {
		t.Fatalf("LanguageByCode(fr) Native lost: %q", fr.Native)
	}
}

// TestBuildSnapshotKeepsAgentOutputCodeKeys ensures the agent-output
// code (a third active surface beyond CLI / TUI) survives the trim.
// Without that, downstream paths that resolve the agent-output
// catalog would see empty Keys after a snapshot rebuild.
func TestBuildSnapshotKeepsAgentOutputCodeKeys(t *testing.T) {
	bundle := Bundle{
		Workflows: []Workflow{{
			ID: 1, Key: "demo", Name: "Demo",
			Buckets: []Bucket{{ID: 1, Key: "backlog", Name: "Backlog", Position: 1}},
		}},
		Config: Settings{
			Workflow:  WorkflowSettings{Active: "demo"},
			Languages: LanguageSettings{CLI: "en", TUI: "en", AgentOutput: "de"},
		},
		Languages: []Language{
			{Code: "en", Name: "English", Native: "English", Keys: map[string]string{"k": "english"}},
			{Code: "de", Name: "German", Native: "Deutsch", Keys: map[string]string{"k": "german"}},
		},
	}
	snap := BuildSnapshot(bundle)
	de, ok := snap.LanguageByCode("de")
	if !ok || len(de.Keys) == 0 {
		t.Fatalf("agent-output code de should keep Keys; ok=%v keys=%d", ok, len(de.Keys))
	}
}
