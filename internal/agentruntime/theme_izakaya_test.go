package agentruntime

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/agent"
)

// openIzakayaRuntime materialises a runtime seeded from the embedded default
// kit with the izakaya preset active (the config basename selects the preset),
// so every ResolveCommand below renders against the Howl's Moving Castle
// themed bundle authored in defaults/config/izakaya.yaml.
func openIzakayaRuntime(t *testing.T) *Runtime {
	t.Helper()
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "data", "omakiten.db")
	configPath := filepath.Join(tmp, "config", "izakaya.yaml")
	rt, err := Open(ctx, Options{DBPath: dbPath, ConfigPath: configPath, CWD: tmp})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	return rt
}

// TestIzakayaBuilderIdentityRenders is the AC#7 anchor: the Builder command
// okt-task-implement must render Howl Pendragon's identity, his command-level
// skill subset as bullet-with-body, the themed walking-skeleton-first law, the
// pull-request template (with the JIT fetch hint), and a non-empty action.
func TestIzakayaBuilderIdentityRenders(t *testing.T) {
	rt := openIzakayaRuntime(t)
	ctx := context.Background()

	resp, err := rt.Service().ResolveCommand(ctx, agent.ResolveCommandInput{Name: "okt-task-implement"})
	if err != nil {
		t.Fatalf("ResolveCommand(okt-task-implement) error = %v", err)
	}

	// Builder persona is Howl.
	if resp.Persona == nil || resp.Persona.Slug != "howl-pendragon" {
		t.Fatalf("okt-task-implement persona = %+v, want slug howl-pendragon", resp.Persona)
	}
	if !strings.Contains(resp.Markdown, "## Persona — ") || !strings.Contains(resp.Markdown, "Howl") {
		t.Fatalf("okt-task-implement markdown missing Howl persona identity:\n%s", resp.Markdown)
	}

	// Themed Builder law renders.
	if !lawPresent(resp.Laws, "walking-skeleton-first") {
		t.Fatalf("okt-task-implement missing themed law walking-skeleton-first; laws = %v", lawSlugs(resp.Laws))
	}
	if !strings.Contains(resp.Markdown, "Walking skeleton first") {
		t.Fatalf("okt-task-implement markdown missing themed law body:\n%s", resp.Markdown)
	}

	// Skills render bullet-with-body.
	assertBulletWithBody(t, "okt-task-implement", resp)

	// Template bound with JIT fetch hint.
	if len(resp.Templates) == 0 {
		t.Fatalf("okt-task-implement binds no templates; expected pull-request")
	}
	if !strings.Contains(resp.Markdown, "## Templates\n") || !strings.Contains(resp.Markdown, "templates.show") {
		t.Fatalf("okt-task-implement missing Templates section or JIT fetch hint:\n%s", resp.Markdown)
	}

	// Non-empty action.
	if !strings.Contains(resp.Markdown, "## Action\n") || strings.TrimSpace(resp.Action) == "" {
		t.Fatalf("okt-task-implement missing non-empty Action:\n%s", resp.Markdown)
	}
}

// TestIzakayaRepresentativeCommandsRender walks one command per themed role
// slot and asserts each resolves the expected persona and renders its
// command-level skill subset as bullet-with-body, plus a wired Action and the
// global Laws floor. This is the per-preset breadth smoke (AC#7) covering the
// dual-bound roster (Calcifer Concierge/Tester, Markl Owner/Committer, Sophie
// Reviewer/Scribe) and the themed Ideator law.
func TestIzakayaRepresentativeCommandsRender(t *testing.T) {
	rt := openIzakayaRuntime(t)
	ctx := context.Background()

	cases := []struct {
		command string
		persona string
		law     string // optional themed/role law that must be present ("" = skip)
	}{
		{"okt", "calcifer", ""},                                          // Concierge
		{"okt-shape", "markl", ""},                                       // Owner orchestrator
		{"okt-task-imagine", "witch-of-the-waste", ""},                   // Ideator
		{"okt-task-validate", "witch-of-the-waste", "cheap-probe-first"}, // Ideator + themed law
		{"okt-task-check", "calcifer", ""},                               // Tester
		{"okt-task-review", "sophie-hatter", ""},                         // Reviewer
		{"okt-task-commit", "markl", ""},                                 // Committer
		{"okt-task-document", "sophie-hatter", ""},                       // Scribe
	}

	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			resp, err := rt.Service().ResolveCommand(ctx, agent.ResolveCommandInput{Name: tc.command})
			if err != nil {
				t.Fatalf("ResolveCommand(%s) error = %v", tc.command, err)
			}
			if resp.Persona == nil || resp.Persona.Slug != tc.persona {
				t.Fatalf("%s persona = %+v, want slug %q", tc.command, resp.Persona, tc.persona)
			}
			if !strings.Contains(resp.Markdown, "## Persona — ") {
				t.Fatalf("%s missing Persona section:\n%s", tc.command, resp.Markdown)
			}
			if !strings.Contains(resp.Markdown, "## Action\n") || strings.TrimSpace(resp.Action) == "" {
				t.Fatalf("%s missing non-empty Action:\n%s", tc.command, resp.Markdown)
			}
			if !strings.Contains(resp.Markdown, "## Laws\n") || len(resp.Laws) == 0 {
				t.Fatalf("%s missing Laws floor:\n%s", tc.command, resp.Markdown)
			}
			assertBulletWithBody(t, tc.command, resp)
			if tc.law != "" && !lawPresent(resp.Laws, tc.law) {
				t.Fatalf("%s missing expected law %q; laws = %v", tc.command, tc.law, lawSlugs(resp.Laws))
			}
		})
	}
}

// TestIzakayaNotesSlotsCarryScribeRepertoire is the AC#6 anchor: every
// notes-bearing slot (okt-pause + okt-note-*) binds the resolved Scribe
// (Sophie) and resolves a #359 note skill subset that renders bullet-with-body.
func TestIzakayaNotesSlotsCarryScribeRepertoire(t *testing.T) {
	rt := openIzakayaRuntime(t)
	ctx := context.Background()

	noteSlots := []string{"okt-pause", "okt-note-free", "okt-note-recap", "okt-note-list", "okt-note-show"}
	for _, name := range noteSlots {
		t.Run(name, func(t *testing.T) {
			resp, err := rt.Service().ResolveCommand(ctx, agent.ResolveCommandInput{Name: name})
			if err != nil {
				t.Fatalf("ResolveCommand(%s) error = %v", name, err)
			}
			if resp.Persona == nil || resp.Persona.Slug != "sophie-hatter" {
				t.Fatalf("%s persona = %+v, want the resolved Scribe sophie-hatter", name, resp.Persona)
			}
			if len(resp.Skills) == 0 {
				t.Fatalf("%s resolved no skills — the #359 note subset is not wired", name)
			}
			noteSkills := map[string]struct{}{
				"handoff-synthesis": {}, "note-capture": {}, "standup-digest": {}, "recap-timeline": {},
			}
			found := false
			for _, sk := range resp.Skills {
				if _, ok := noteSkills[sk.Slug]; ok {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("%s resolved no #359 note skill; skills = %v", name, skillSlugs(resp.Skills))
			}
			assertBulletWithBody(t, name, resp)
		})
	}
}

// --- helpers ---

func assertBulletWithBody(t *testing.T, name string, resp agent.ResolveCommandResponse) {
	t.Helper()
	if len(resp.Skills) == 0 {
		t.Fatalf("%s resolved with no skills — command-level subset not wired", name)
	}
	if !strings.Contains(resp.Markdown, "## Skills\n") {
		t.Fatalf("%s missing Skills section despite %d skills:\n%s", name, len(resp.Skills), resp.Markdown)
	}
	for _, sk := range resp.Skills {
		label := sk.Name
		if label == "" {
			label = sk.Slug
		}
		body := strings.TrimSpace(sk.Body)
		if body == "" {
			body = strings.TrimSpace(sk.Description)
		}
		if body == "" {
			t.Fatalf("%s skill %q renders as a bare name bullet — bullet-with-body requires a body", name, label)
		}
		head := body
		if idx := strings.IndexByte(head, '\n'); idx >= 0 {
			head = head[:idx]
		}
		wantBullet := "- **" + label + "** — " + head
		if !strings.Contains(resp.Markdown, wantBullet) {
			t.Fatalf("%s skill %q did not render bullet-with-body (expected %q):\n%s", name, label, wantBullet, resp.Markdown)
		}
	}
}
