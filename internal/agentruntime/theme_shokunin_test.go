package agentruntime

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/agent"
)

// openShokunin boots the runtime against the embedded shokunin default kit by
// pointing ConfigPath at the materialized shokunin.yaml. EnsureDefaultFiles
// writes every embedded preset into config/, so naming the shokunin profile
// here selects the FMA Brotherhood themed bundle (personas/laws/skills) rather
// than the omakase reference kit the shared gates use.
func openShokunin(t *testing.T) *Runtime {
	t.Helper()
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "data", "omakiten.db")
	configPath := filepath.Join(tmp, "config", "shokunin.yaml")

	rt, err := Open(ctx, Options{DBPath: dbPath, ConfigPath: configPath, CWD: tmp})
	if err != nil {
		t.Fatalf("Open(shokunin) error = %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	return rt
}

// resolveShokunin resolves one command against the shokunin kit.
func resolveShokunin(t *testing.T, rt *Runtime, name string) agent.ResolveCommandResponse {
	t.Helper()
	resp, err := rt.Service().ResolveCommand(context.Background(), agent.ResolveCommandInput{Name: name})
	if err != nil {
		t.Fatalf("ResolveCommand(%s) error = %v", name, err)
	}
	return resp
}

// assertSkillsBulletWithBody pins the W4 theming contract for a resolved
// command: every declared skill renders as a `- **Name** — body` bullet under
// `## Skills` (never a bare name or an empty section).
func assertSkillsBulletWithBody(t *testing.T, name string, resp agent.ResolveCommandResponse) {
	t.Helper()
	if len(resp.Skills) == 0 {
		t.Fatalf("%s resolved with no skills — the command-level skill subset is not wired", name)
	}
	if !strings.Contains(resp.Markdown, "## Skills\n") {
		t.Fatalf("%s markdown missing the Skills section despite %d resolved skills:\n%s", name, len(resp.Skills), resp.Markdown)
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
			t.Fatalf("%s skill %q renders as a bare name bullet — bullet-with-body requires a non-empty body or description", name, label)
		}
		head := body
		if idx := strings.IndexByte(head, '\n'); idx >= 0 {
			head = head[:idx]
		}
		wantBullet := "- **" + label + "** — " + head
		if !strings.Contains(resp.Markdown, wantBullet) {
			t.Fatalf("%s skill %q did not render bullet-with-body (expected line %q):\n%s", name, label, wantBullet, resp.Markdown)
		}
	}
}

// TestShokuninPresetSmoke is the AC#7 per-preset smoke gate. It renders a
// representative slice of the shokunin (FMA Brotherhood) command surface against
// the embedded shokunin kit and asserts each one carries its expected sections:
// a wired themed Persona, a non-empty Action + prompts/list description, a Laws
// section (the global floor reaches every command), and bullet-with-body Skills.
// It is the shokunin sibling of the omakase TestFullCommandSurfaceSmoke — scoped
// to the representative set named in AC#7, not the full 40, so it stays cheap and
// preset-local while still proving the themed wiring resolves end-to-end.
func TestShokuninPresetSmoke(t *testing.T) {
	rt := openShokunin(t)

	// Representative slice spanning every canonical role: Concierge entry,
	// Owner orchestrator, Ideator, Builder loop, Tester, Reviewer, Committer,
	// Scribe + notes.
	representative := []string{
		"okt",
		"okt-start",
		"okt-shape",
		"okt-run",
		"okt-task-imagine",
		"okt-task-create",
		"okt-task-implement",
		"okt-task-review",
		"okt-task-check",
		"okt-task-commit",
		"okt-task-document",
		"okt-pause",
		"okt-note-recap",
	}

	for _, name := range representative {
		t.Run(name, func(t *testing.T) {
			resp := resolveShokunin(t, rt, name)

			if resp.Persona == nil {
				t.Fatalf("%s resolved with no persona — the role slot is not wired in shokunin.yaml", name)
			}
			if !strings.Contains(resp.Markdown, "## Persona — ") {
				t.Fatalf("%s markdown missing non-empty Persona section:\n%s", name, resp.Markdown)
			}
			if !strings.Contains(resp.Markdown, "## Action\n") || strings.TrimSpace(resp.Action) == "" {
				t.Fatalf("%s markdown missing non-empty Action section:\n%s", name, resp.Markdown)
			}
			if strings.TrimSpace(agent.CommandDescription(name)) == "" {
				t.Fatalf("%s carries no prompts/list description", name)
			}
			if !strings.Contains(resp.Markdown, "## Laws\n") || len(resp.Laws) == 0 {
				t.Fatalf("%s markdown missing non-empty Laws section (the global law floor should reach every command):\n%s", name, resp.Markdown)
			}
			assertSkillsBulletWithBody(t, name, resp)

			if len(resp.Templates) > 0 {
				if !strings.Contains(resp.Markdown, "## Templates\n") {
					t.Fatalf("%s binds %d template(s) but renders no Templates section:\n%s", name, len(resp.Templates), resp.Markdown)
				}
				if !strings.Contains(resp.Markdown, "templates.show") {
					t.Fatalf("%s binds templates but carries no templates.show JIT fetch hint:\n%s", name, resp.Markdown)
				}
			}
		})
	}
}

// TestShokuninBuilderIdentity pins AC#7's Builder-identity requirement: the
// okt-task-implement Builder loop must resolve to Edward Elric, render his TONE
// body (the equivalent-exchange identity), carry the bullet-with-body Builder
// skill subset, surface a themed law, and bind its templates with the JIT hint.
func TestShokuninBuilderIdentity(t *testing.T) {
	rt := openShokunin(t)
	resp := resolveShokunin(t, rt, "okt-task-implement")

	if resp.Persona == nil {
		t.Fatal("okt-task-implement resolved with no persona — Builder slot unwired")
	}
	if resp.Persona.Slug != "edward-elric" {
		t.Fatalf("okt-task-implement Builder = %q, want edward-elric", resp.Persona.Slug)
	}
	if resp.Persona.Name != "Edward Elric" {
		t.Fatalf("Builder persona name = %q, want \"Edward Elric\"", resp.Persona.Name)
	}
	// The themed identity body (TONE) must render — pin a load-bearing phrase
	// from Edward's equivalent-exchange identity.
	if !strings.Contains(resp.Markdown, "You are Edward Elric") {
		t.Fatalf("okt-task-implement markdown missing Edward Elric identity body:\n%s", resp.Markdown)
	}
	if !strings.Contains(strings.ToLower(resp.Markdown), "equivalent exchange") {
		t.Fatalf("okt-task-implement markdown missing Edward's equivalent-exchange tone:\n%s", resp.Markdown)
	}

	// Bullet-with-body Builder skills (incl. the themed gate-of-truth-toll +
	// automail-fallback variants).
	assertSkillsBulletWithBody(t, "okt-task-implement", resp)
	wantSkills := map[string]bool{"gate-of-truth-toll": false, "automail-fallback": false, "implementation": false}
	for _, sk := range resp.Skills {
		if _, ok := wantSkills[sk.Slug]; ok {
			wantSkills[sk.Slug] = true
		}
	}
	for slug, seen := range wantSkills {
		if !seen {
			t.Fatalf("okt-task-implement Builder subset missing skill %q:\n%v", slug, resp.Skills)
		}
	}

	// Themed law — the transmutation circle (pre-mortem) must reach the Builder
	// loop and render its body.
	if !lawPresent(resp.Laws, "transmutation-circle-required") {
		t.Fatalf("okt-task-implement missing themed law transmutation-circle-required:\n%v", lawSlugs(resp.Laws))
	}
	if !strings.Contains(resp.Markdown, "Transmutation circle required") {
		t.Fatalf("okt-task-implement markdown missing the transmutation-circle law body:\n%s", resp.Markdown)
	}

	// Templates bound with the JIT fetch hint.
	if len(resp.Templates) == 0 {
		t.Fatal("okt-task-implement binds no templates")
	}
	if !strings.Contains(resp.Markdown, "templates.show") {
		t.Fatalf("okt-task-implement binds templates but carries no templates.show JIT hint:\n%s", resp.Markdown)
	}

	// Action text lands.
	if strings.TrimSpace(resp.Action) == "" {
		t.Fatal("okt-task-implement carries empty action text")
	}
}

// TestShokuninThemedLawsAndReviewGate pins AC#3: the Reviewer gate renders the
// themed briggs-fortress-gate + equivalent-exchange-audit laws (bodies present),
// and the Owner hard-gate orchestrator (okt-audit) seats the locked king-bradley
// authority on the briggs-fortress-gate law.
func TestShokuninThemedLawsAndReviewGate(t *testing.T) {
	rt := openShokunin(t)

	review := resolveShokunin(t, rt, "okt-task-review")
	if review.Persona == nil || review.Persona.Slug != "olivier-armstrong" {
		t.Fatalf("okt-task-review Reviewer = %v, want olivier-armstrong", review.Persona)
	}
	for _, slug := range []string{"briggs-fortress-gate", "equivalent-exchange-audit"} {
		if !lawPresent(review.Laws, slug) {
			t.Fatalf("okt-task-review missing themed law %q:\n%v", slug, lawSlugs(review.Laws))
		}
	}
	if !strings.Contains(review.Markdown, "Briggs fortress gate") {
		t.Fatalf("okt-task-review markdown missing briggs-fortress-gate law body:\n%s", review.Markdown)
	}

	audit := resolveShokunin(t, rt, "okt-audit")
	if audit.Persona == nil || audit.Persona.Slug != "king-bradley" {
		t.Fatalf("okt-audit authority = %v, want king-bradley (locked roster)", audit.Persona)
	}
	if !lawPresent(audit.Laws, "briggs-fortress-gate") {
		t.Fatalf("okt-audit missing themed law briggs-fortress-gate:\n%v", lawSlugs(audit.Laws))
	}
}

// TestShokuninNotesSlotsBindScribe pins AC#6: okt-pause and every okt-note-*
// slot bind the Scribe (alphonse-elric), whose repertoire carries the four #359
// note skills, so each notes-bearing slot resolves a real skill subset.
func TestShokuninNotesSlotsBindScribe(t *testing.T) {
	rt := openShokunin(t)

	noteSlots := map[string]string{
		"okt-pause":      "handoff-synthesis",
		"okt-note-free":  "note-capture",
		"okt-note-recap": "recap-timeline",
		"okt-note-list":  "note-capture",
		"okt-note-show":  "note-capture",
	}
	for name, wantSkill := range noteSlots {
		t.Run(name, func(t *testing.T) {
			resp := resolveShokunin(t, rt, name)
			if resp.Persona == nil || resp.Persona.Slug != "alphonse-elric" {
				t.Fatalf("%s Scribe = %v, want alphonse-elric", name, resp.Persona)
			}
			found := false
			for _, sk := range resp.Skills {
				if sk.Slug == wantSkill {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s missing expected #359 note skill %q in resolved subset:\n%v", name, wantSkill, resp.Skills)
			}
			assertSkillsBulletWithBody(t, name, resp)
		})
	}
}
