package agentruntime

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/contract"
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

	resp, err := rt.Service().ResolveCommand(ctx, contract.ResolveCommandInput{Name: "okt-task-implement"})
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
	assertCommandSkills(t, "okt-task-implement", resp)

	// Template bound with JIT fetch hint.
	if len(resp.Templates) == 0 {
		t.Fatalf("okt-task-implement binds no templates; expected pull-request")
	}
	if !strings.Contains(resp.Markdown, "## Templates\n") || !strings.Contains(resp.Markdown, "okt template show") {
		t.Fatalf("okt-task-implement missing Templates section or JIT fetch hint:\n%s", resp.Markdown)
	}

	// Entity-sourced playbook renders under Skills (no hardcoded Action).
	if !strings.Contains(resp.Markdown, "## Skills\n") {
		t.Fatalf("okt-task-implement missing the Skills section carrying the entity-sourced playbook:\n%s", resp.Markdown)
	}
}

// TestIzakayaRepresentativeCommandsRender walks one command per themed role
// slot and asserts each resolves the expected persona and renders its
// command-level skill subset as bullet-with-body, plus the entity-sourced
// playbook in the Skills section and the
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
			assertIzakayaRepresentative(t, ctx, rt, tc)
		})
	}
}

func assertIzakayaRepresentative(t *testing.T, ctx context.Context, rt *Runtime, tc struct {
	command string
	persona string
	law     string
}) {
	t.Helper()
	resp, err := rt.Service().ResolveCommand(ctx, contract.ResolveCommandInput{Name: tc.command})
	if err != nil {
		t.Fatalf("ResolveCommand(%s) error = %v", tc.command, err)
	}
	if resp.Persona == nil || resp.Persona.Slug != tc.persona {
		t.Fatalf("%s persona = %+v, want slug %q", tc.command, resp.Persona, tc.persona)
	}
	if !strings.Contains(resp.Markdown, "## Persona — ") {
		t.Fatalf("%s missing Persona section:\n%s", tc.command, resp.Markdown)
	}
	if !strings.Contains(resp.Markdown, "## Skills\n") {
		t.Fatalf("%s missing the Skills section carrying the entity-sourced playbook:\n%s", tc.command, resp.Markdown)
	}
	if !strings.Contains(resp.Markdown, "## Laws\n") || len(resp.Laws) == 0 {
		t.Fatalf("%s missing Laws floor:\n%s", tc.command, resp.Markdown)
	}
	assertCommandSkills(t, tc.command, resp)
	if tc.law != "" && !lawPresent(resp.Laws, tc.law) {
		t.Fatalf("%s missing expected law %q; laws = %v", tc.command, tc.law, lawSlugs(resp.Laws))
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
			assertIzakayaNoteSlot(t, ctx, rt, name)
		})
	}
}

func assertIzakayaNoteSlot(t *testing.T, ctx context.Context, rt *Runtime, name string) {
	t.Helper()
	resp, err := rt.Service().ResolveCommand(ctx, contract.ResolveCommandInput{Name: name})
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
	if !hasSkill(resp.Skills, noteSkills) {
		t.Fatalf("%s resolved no #359 note skill; skills = %v", name, skillSlugs(resp.Skills))
	}
	assertCommandSkills(t, name, resp)
}

func hasSkill(skills []contract.SkillInfo, want map[string]struct{}) bool {
	for _, skill := range skills {
		if _, ok := want[skill.Slug]; ok {
			return true
		}
	}
	return false
}

// --- helpers ---
