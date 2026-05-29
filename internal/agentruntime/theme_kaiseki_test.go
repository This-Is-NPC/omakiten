package agentruntime

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/agent"
)

// openKaiseki boots a runtime against the embedded kaiseki (LOTR) default kit
// and resolves one command, failing the test on any error. Pointing ConfigPath
// at `kaiseki.yaml` selects the kaiseki bundle: EnsureDefaultFiles seeds every
// preset, and resolvedConfigPath picks the named active config.
func openKaiseki(t *testing.T) *Runtime {
	t.Helper()
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "data", "omakiten.db")
	configPath := filepath.Join(tmp, "config", "kaiseki.yaml")

	rt, err := Open(ctx, Options{DBPath: dbPath, ConfigPath: configPath, CWD: tmp})
	if err != nil {
		t.Fatalf("Open(kaiseki) error = %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	return rt
}

func resolveKaiseki(t *testing.T, rt *Runtime, name string) agent.ResolveCommandResponse {
	t.Helper()
	resp, err := rt.Service().ResolveCommand(context.Background(), agent.ResolveCommandInput{Name: name})
	if err != nil {
		t.Fatalf("ResolveCommand(%s) error = %v", name, err)
	}
	return resp
}

// TestKaisekiAragornBuilderIdentity is the AC#7 per-preset smoke: the Builder
// slots must resolve the themed Aragorn persona and render its TONE body, the
// declared skill subset must ship as bullet-with-body, the implement loop must
// carry its themed law (Map before the road) and a bound template's JIT fetch
// hint, and the action text must land. This pins the load-bearing pieces so a
// regression that drops the persona body, the skill bullets, the themed law, or
// the template hint surfaces here.
func TestKaisekiAragornBuilderIdentity(t *testing.T) {
	rt := openKaiseki(t)
	resp := resolveKaiseki(t, rt, "okt-task-implement")

	// Persona — themed Builder slot wired to Aragorn, body is rendered TONE.
	if resp.Persona == nil {
		t.Fatalf("okt-task-implement resolved with no persona — the Builder slot is not wired")
	}
	if resp.Persona.Slug != "aragorn" {
		t.Fatalf("okt-task-implement Builder persona = %q, want aragorn", resp.Persona.Slug)
	}
	if !strings.Contains(resp.Markdown, "## Persona — Aragorn") {
		t.Fatalf("okt-task-implement markdown missing Aragorn persona header:\n%s", resp.Markdown)
	}
	if !strings.Contains(resp.Markdown, "You are Aragorn") {
		t.Fatalf("okt-task-implement markdown missing Aragorn TONE body:\n%s", resp.Markdown)
	}

	// Action — non-empty section + field.
	if !strings.Contains(resp.Markdown, "## Action\n") || strings.TrimSpace(resp.Action) == "" {
		t.Fatalf("okt-task-implement markdown missing non-empty Action section:\n%s", resp.Markdown)
	}

	// Skills — bullet-with-body for the declared subset.
	if len(resp.Skills) == 0 {
		t.Fatalf("okt-task-implement resolved with no skills — the command-level subset is not wired")
	}
	if !strings.Contains(resp.Markdown, "## Skills\n") {
		t.Fatalf("okt-task-implement markdown missing the Skills section:\n%s", resp.Markdown)
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
			t.Fatalf("okt-task-implement skill %q renders as a bare name bullet", label)
		}
		head := body
		if idx := strings.IndexByte(head, '\n'); idx >= 0 {
			head = head[:idx]
		}
		wantBullet := "- **" + label + "** — " + head
		if !strings.Contains(resp.Markdown, wantBullet) {
			t.Fatalf("okt-task-implement skill %q did not render bullet-with-body (expected %q):\n%s", label, wantBullet, resp.Markdown)
		}
	}

	// Themed law — "Map before the road" must reach the implement loop and
	// render its neutral SE body.
	if !lawPresent(resp.Laws, "map-before-the-road") {
		t.Fatalf("okt-task-implement missing themed law map-before-the-road; got %v", lawSlugs(resp.Laws))
	}
	if !strings.Contains(resp.Markdown, "Map before the road") {
		t.Fatalf("okt-task-implement markdown missing themed law name 'Map before the road':\n%s", resp.Markdown)
	}

	// Templates — bound, with the JIT fetch hint.
	if len(resp.Templates) == 0 {
		t.Fatalf("okt-task-implement binds no templates")
	}
	if !strings.Contains(resp.Markdown, "## Templates\n") || !strings.Contains(resp.Markdown, "templates.show") {
		t.Fatalf("okt-task-implement missing Templates section or templates.show JIT hint:\n%s", resp.Markdown)
	}
}

// TestKaisekiRosterBindings pins the canonical-role → themed-persona matrix so a
// future edit that rebinds a slot to the wrong hand surfaces here. Covers one
// representative command per role across the LOTR roster.
func TestKaisekiRosterBindings(t *testing.T) {
	rt := openKaiseki(t)
	cases := map[string]string{
		"okt-start":          "bilbo-baggins",    // Concierge
		"okt-shape":          "gandalf-the-grey", // Owner orchestrator
		"okt-task-imagine":   "frodo-baggins",    // Ideator
		"okt-task-implement": "aragorn",          // Builder
		"okt-task-check":     "legolas",          // Tester
		"okt-task-review":    "elrond",           // Reviewer
		"okt-task-commit":    "samwise-gamgee",   // Committer
		"okt-task-document":  "bilbo-baggins",    // Scribe
	}
	for name, wantPersona := range cases {
		t.Run(name, func(t *testing.T) {
			resp := resolveKaiseki(t, rt, name)
			if resp.Persona == nil {
				t.Fatalf("%s resolved with no persona", name)
			}
			if resp.Persona.Slug != wantPersona {
				t.Fatalf("%s persona = %q, want %q", name, resp.Persona.Slug, wantPersona)
			}
			// Every command renders a wired persona section, a non-empty
			// action, and at least one bullet-with-body skill.
			if !strings.Contains(resp.Markdown, "## Persona — ") {
				t.Fatalf("%s markdown missing Persona section:\n%s", name, resp.Markdown)
			}
			if strings.TrimSpace(resp.Action) == "" {
				t.Fatalf("%s resolved with empty action", name)
			}
			if len(resp.Skills) == 0 {
				t.Fatalf("%s resolved with no skills — subset not wired", name)
			}
		})
	}
}

// TestKaisekiNotesSlotsScribeRepertoire pins AC#6: the notes-bearing slots bind
// the Concierge+Scribe (bilbo-baggins) and each resolves a real skill from a
// repertoire that carries the #359 note skills. okt-pause additionally ships
// the themed chronicle-handoff skill, the themed Red Book of Westmarch law, and
// the note-handoff template.
func TestKaisekiNotesSlotsScribeRepertoire(t *testing.T) {
	rt := openKaiseki(t)
	notesSlots := []string{"okt-pause", "okt-note-free", "okt-note-recap", "okt-note-list", "okt-note-show"}
	for _, name := range notesSlots {
		t.Run(name, func(t *testing.T) {
			resp := resolveKaiseki(t, rt, name)
			if resp.Persona == nil || resp.Persona.Slug != "bilbo-baggins" {
				t.Fatalf("%s persona = %v, want bilbo-baggins", name, resp.Persona)
			}
			if len(resp.Skills) == 0 {
				t.Fatalf("%s notes slot resolved with no skill — repertoire does not carry the bound skill", name)
			}
		})
	}

	// okt-pause: themed chronicle skill + themed law + template.
	pause := resolveKaiseki(t, rt, "okt-pause")
	if !skillPresent(pause.Skills, "chronicle-handoff") {
		t.Fatalf("okt-pause missing themed chronicle-handoff skill; got %v", skillSlugs(pause.Skills))
	}
	if !lawPresent(pause.Laws, "red-book-of-westmarch") {
		t.Fatalf("okt-pause missing themed law red-book-of-westmarch; got %v", lawSlugs(pause.Laws))
	}
	if !strings.Contains(pause.Markdown, "Red Book of Westmarch") {
		t.Fatalf("okt-pause markdown missing themed law name 'Red Book of Westmarch':\n%s", pause.Markdown)
	}
	if len(pause.Templates) == 0 || !strings.Contains(pause.Markdown, "templates.show") {
		t.Fatalf("okt-pause missing note-handoff template or templates.show hint:\n%s", pause.Markdown)
	}
}

// TestKaisekiReviewerCouncilLaw pins that the themed Council of Elrond review
// law reaches the Reviewer and Builder loops with its neutral SE body.
func TestKaisekiReviewerCouncilLaw(t *testing.T) {
	rt := openKaiseki(t)
	resp := resolveKaiseki(t, rt, "okt-task-review")
	if !lawPresent(resp.Laws, "council-of-elrond-review") {
		t.Fatalf("okt-task-review missing themed law council-of-elrond-review; got %v", lawSlugs(resp.Laws))
	}
	if !strings.Contains(resp.Markdown, "Council of Elrond review") {
		t.Fatalf("okt-task-review markdown missing themed law name 'Council of Elrond review':\n%s", resp.Markdown)
	}
}

func skillPresent(skills []agent.SkillInfo, slug string) bool {
	for _, s := range skills {
		if s.Slug == slug {
			return true
		}
	}
	return false
}
