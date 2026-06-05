package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolveCommandComposesEffectiveLaws is the load-bearing test for the
// guardrails-wiring contract: global laws apply to every command, persona +
// command + template laws layer on top, and laws_disabled removes inherited
// entries. We verify both the structured Laws slice and the rendered
// markdown so regressions on either side surface immediately.
func TestResolveCommandComposesEffectiveLaws(t *testing.T) {
	fixture := newAgentFixture(t)
	wireBindingFixtures(t, fixture)

	resp, err := fixture.service.ResolveCommand(fixture.ctx, ResolveCommandInput{Name: "okt-task-implement"})
	if err != nil {
		t.Fatalf("ResolveCommand() error = %v", err)
	}
	if resp.Persona == nil || resp.Persona.Slug != "backend-agent" {
		t.Fatalf("ResolveCommand persona = %+v, want backend-agent", resp.Persona)
	}
	if len(resp.Skills) != 1 || resp.Skills[0].Slug != "go" {
		t.Fatalf("ResolveCommand skills = %+v, want only go", resp.Skills)
	}
	if len(resp.Templates) != 1 || resp.Templates[0].Slug != "pull-request" {
		t.Fatalf("ResolveCommand templates = %+v, want only pull-request", resp.Templates)
	}
	gotSlugs := []string{}
	for _, l := range resp.Laws {
		gotSlugs = append(gotSlugs, l.Slug)
	}
	wantSlugs := []string{"template-fidelity", "project-scope-only"}
	if !equalStringSlices(gotSlugs, wantSlugs) {
		t.Fatalf("ResolveCommand laws = %v, want %v (global ∪ persona, deduped)", gotSlugs, wantSlugs)
	}
	if !strings.Contains(resp.Markdown, "## Laws\n") {
		t.Fatalf("Markdown should headline the laws section, got:\n%s", resp.Markdown)
	}
	if strings.Contains(resp.Markdown, "## Laws (") {
		t.Fatalf("Markdown should not carry the (count) parenthetical on the Laws header, got:\n%s", resp.Markdown)
	}
}

// TestResolveCommandLawsDisabledOptsOut covers the okt-imagine case where the
// command opts out of the global template-fidelity law. The disabled slug must
// be absent from the resolved set even though it is declared in the global
// spec, and the persona's project-scope-only law must still be present.
func TestResolveCommandLawsDisabledOptsOut(t *testing.T) {
	fixture := newAgentFixture(t)
	wireBindingFixtures(t, fixture)

	resp, err := fixture.service.ResolveCommand(fixture.ctx, ResolveCommandInput{Name: "okt-task-imagine"})
	if err != nil {
		t.Fatalf("ResolveCommand() error = %v", err)
	}
	for _, l := range resp.Laws {
		if l.Slug == "template-fidelity" {
			t.Fatalf("template-fidelity must be opted out by laws_disabled, got laws = %+v", resp.Laws)
		}
	}
	hasScope := false
	for _, l := range resp.Laws {
		if l.Slug == "project-scope-only" {
			hasScope = true
		}
	}
	if !hasScope {
		t.Fatalf("ResolveCommand should still expose persona laws when only template-fidelity is disabled, got %+v", resp.Laws)
	}
}

// TestResolveCommandUnknownCommand checks that an unknown prompt name surfaces
// a validation error rather than a partial response.
func TestResolveCommandUnknownCommand(t *testing.T) {
	fixture := newAgentFixture(t)
	wireBindingFixtures(t, fixture)

	if _, err := fixture.service.ResolveCommand(fixture.ctx, ResolveCommandInput{Name: "okt-bogus"}); err == nil {
		t.Fatal("ResolveCommand(unknown) error = nil, want validation failure")
	}
}

// TestResolveCommandEmptyName covers the empty-input boundary.
func TestResolveCommandEmptyName(t *testing.T) {
	fixture := newAgentFixture(t)
	if _, err := fixture.service.ResolveCommand(fixture.ctx, ResolveCommandInput{Name: ""}); err == nil {
		t.Fatal("ResolveCommand(empty) error = nil, want validation failure")
	}
}

// The REST-style handoff contract (every command's playbook names the next
// command in the cycle) is now an entity-sourced property of the bound
// okt-<slug>-playbook skills, asserted against the rendered default kit by
// agentruntime.TestRestHandoffsPresent — the Go layer no longer carries the
// action prose to check here.

// TestResolveCommandTemplatesJITRendering pins the just-in-time pattern:
// when a command has a bound template, the rendered Markdown must list the
// template metadata (slug, name when divergent, default kind, description),
// but it must NOT embed the template body. Embedding the body would defeat
// the entire point of JIT — the body is large and the agent only needs it
// at the moment of materialization.
//
// The `templates.show` fetch hint itself is covered against the default kit
// by `agentruntime.TestTemplateBoundCommandsCarryFetchHint`, which asserts
// every command that binds templates surfaces the hint via its action text or
// persona body.
func TestResolveCommandTemplatesJITRendering(t *testing.T) {
	fixture := newAgentFixture(t)
	wireBindingFixtures(t, fixture)

	resp, err := fixture.service.ResolveCommand(fixture.ctx, ResolveCommandInput{Name: "okt-task-implement"})
	if err != nil {
		t.Fatalf("ResolveCommand() error = %v", err)
	}
	if !strings.Contains(resp.Markdown, "## Templates") {
		t.Fatal("Markdown missing ## Templates section")
	}
	// Pull-request body fixture starts with `## Before` — that body must NOT
	// appear inline. If it does, the renderer regressed to embedding bodies.
	if strings.Contains(resp.Markdown, "## Before") {
		t.Fatalf("Template body leaked into Markdown — JIT contract broken:\n%s", resp.Markdown)
	}
	// Slug must still be present so the agent knows which template to fetch.
	if !strings.Contains(resp.Markdown, "pull-request") {
		t.Fatal("Markdown missing template slug — agent has no anchor for templates.show")
	}
}

// TestRenderCommandMarkdownDropsRedundantStructure pins the renderer
// streamlining contract: the rendered Markdown must NOT echo the prompt name
// header or description (both ship in `prompts/list` metadata). Skills render
// as bullet-with-body — one bullet per skill, in configured order, carrying
// the skill body when present and falling back to the description otherwise.
// The bare `## Skills — A, B` inline header (no per-skill detail) is gone.
func TestRenderCommandMarkdownDropsRedundantStructure(t *testing.T) {
	fixture := newAgentFixture(t)
	wireBindingFixtures(t, fixture)

	resp, err := fixture.service.ResolveCommand(fixture.ctx, ResolveCommandInput{Name: "okt-task-implement"})
	if err != nil {
		t.Fatalf("ResolveCommand() error = %v", err)
	}
	// No H1 header echoing the prompt name.
	if strings.HasPrefix(resp.Markdown, "# ") || strings.Contains(resp.Markdown, "\n# okt-") {
		t.Fatalf("Markdown should not echo prompt name as H1 header — already in prompts/list metadata. Got:\n%s", resp.Markdown)
	}
	// The section headline carries no inline name list anymore — detail lives
	// in the per-skill bullets below it.
	if !strings.Contains(resp.Markdown, "## Skills\n") {
		t.Fatalf("Markdown should carry a plain `## Skills` header, got:\n%s", resp.Markdown)
	}
	if strings.Contains(resp.Markdown, "## Skills — ") {
		t.Fatalf("Markdown should not carry the inline `## Skills — <names>` list, got:\n%s", resp.Markdown)
	}
	// The okt-implement persona binds only `go`, which has a body in the
	// fixture — it must render bullet-with-body.
	if !strings.Contains(resp.Markdown, "- **Go** — Go body.") {
		t.Fatalf("Markdown should render skill body bullet `- **Go** — Go body.`, got:\n%s", resp.Markdown)
	}
}

// TestRenderCommandMarkdownSkillBulletWithBody pins the bullet-with-body
// contract directly: skills with a body render the body, skills without a
// body fall back to the description, and configured order is preserved.
func TestRenderCommandMarkdownSkillBulletWithBody(t *testing.T) {
	resp := ResolveCommandResponse{
		Skills: []SkillInfo{
			{Slug: "go", Name: "Go", Description: "Idiomatic Go.", Body: "Write small functions.\nPrefer composition."},
			{Slug: "sqlite", Name: "SQLite", Description: "Embedded SQL.", Body: ""},
			{Slug: "bare", Name: "Bare", Description: "", Body: ""},
		},
	}
	md := renderCommandMarkdown(resp)

	// Body-bearing skill renders its body; multi-line body indents continuations.
	if !strings.Contains(md, "- **Go** — Write small functions.\n  Prefer composition.") {
		t.Fatalf("body-bearing skill should render bullet-with-body, got:\n%s", md)
	}
	// Body-less skill falls back to the description.
	if !strings.Contains(md, "- **SQLite** — Embedded SQL.") {
		t.Fatalf("body-less skill should fall back to description, got:\n%s", md)
	}
	// Skill with neither body nor description renders the bare name.
	if !strings.Contains(md, "- **Bare**\n") {
		t.Fatalf("skill with no body or description should render bare name, got:\n%s", md)
	}
	// Configured order preserved: Go before SQLite before Bare.
	goIdx := strings.Index(md, "**Go**")
	sqliteIdx := strings.Index(md, "**SQLite**")
	bareIdx := strings.Index(md, "**Bare**")
	if goIdx >= sqliteIdx || sqliteIdx >= bareIdx {
		t.Fatalf("skills must render in configured order, got go=%d sqlite=%d bare=%d:\n%s", goIdx, sqliteIdx, bareIdx, md)
	}
}

// TestRenderCommandMarkdownTemplateNameEchoSuppressed pins the rule that the
// human-readable template name is dropped from the bound-template line when
// it is just the title-case of the slug — the slug already carries the
// information. When the name diverges, it stays.
func TestRenderCommandMarkdownTemplateNameEchoSuppressed(t *testing.T) {
	cases := []struct {
		name string
		slug string
		echo bool
	}{
		{"Config Orientation", "config-orientation", true},
		{"Pull Request", "pull-request", true},
		{"User Story", "user-story", true},
		{"Pull Request", "pr", false},
		{"Resume comment", "comment-resume", false},
		{"", "anything", false},
	}
	for _, c := range cases {
		got := templateNameEchoesSlug(c.name, c.slug)
		if got != c.echo {
			t.Errorf("templateNameEchoesSlug(%q, %q) = %t, want %t", c.name, c.slug, got, c.echo)
		}
	}
}

// TestLawBodiesCarryFewShotExamples pins the few-shot pattern Anthropic
// recommends: load-bearing laws should ship at least one Bad:/Good: example
// so the agent learns from concrete cases instead of abstract directives.
// Accidental over-compression of these law bodies would silently strip the
// examples — a regression that is hard to spot without an explicit assertion.
func TestLawBodiesCarryFewShotExamples(t *testing.T) {
	fixture := newAgentFixture(t)
	wireBindingFixtures(t, fixture)

	loadBearing := []string{"template-fidelity"} // wired in the fixture's law catalog
	resp, err := fixture.service.ResolveCommand(fixture.ctx, ResolveCommandInput{Name: "okt-task-implement"})
	if err != nil {
		t.Fatalf("ResolveCommand() error = %v", err)
	}
	for _, slug := range loadBearing {
		var body string
		for _, l := range resp.Laws {
			if l.Slug == slug {
				body = l.Body
				break
			}
		}
		if body == "" {
			t.Fatalf("law %s not present in resolved laws", slug)
		}
		if !strings.Contains(body, "Bad:") || !strings.Contains(body, "Good:") {
			t.Fatalf("law %s body missing Bad:/Good: example markers:\n%s", slug, body)
		}
	}
}

// TestResolveCommandRendersPersonaBody pins the persona-as-instruction-carrier
// contract: when the bound persona has a non-empty body, the rendered Markdown
// must include it under the `## Persona` section. Without this, role-specific
// flow (the implement loop) silently drops out of the prompt.
func TestResolveCommandRendersPersonaBody(t *testing.T) {
	fixture := newAgentFixture(t)
	const marker = "PERSONA_BODY_MARKER_xyz"
	wireBindingFixturesWithPersona(t, fixture, PersonaInfo{
		Slug:        "backend-agent",
		Name:        "Backend Agent",
		Description: "Backend-focused agent.",
		Body:        "Loop step 1.\n" + marker + "\nLoop step 3.",
		Skills:      []string{"go"},
		Laws:        []string{"project-scope-only"},
	})

	resp, err := fixture.service.ResolveCommand(fixture.ctx, ResolveCommandInput{Name: "okt-task-implement"})
	if err != nil {
		t.Fatalf("ResolveCommand() error = %v", err)
	}
	if !strings.Contains(resp.Markdown, marker) {
		t.Fatalf("persona body marker %q missing from rendered markdown:\n%s", marker, resp.Markdown)
	}
}

// TestResolveCommandFallsBackToSkillRepertoire pins the schema-v2 fallback:
// when a command declares no command-level skills AND the bound persona has an
// empty v1 Skills list but a populated v2 SkillRepertoire, the resolver must
// render the repertoire skills under `## Skills`. Before SkillRepertoire was
// carried on PersonaInfo this rendered an empty section silently.
func TestResolveCommandFallsBackToSkillRepertoire(t *testing.T) {
	fixture := newAgentFixture(t)
	wireBindingFixturesWithPersona(t, fixture, PersonaInfo{
		Slug:            "backend-agent",
		Name:            "Backend Agent",
		Description:     "Backend-focused agent.",
		Skills:          nil, // v2 persona: no directly-wired v1 skills
		SkillRepertoire: []string{"go", "sqlite"},
		Laws:            []string{"project-scope-only"},
	})

	// okt-task-implement declares no command-level skills in the fixture, so
	// resolution must walk the fallback chain into SkillRepertoire.
	resp, err := fixture.service.ResolveCommand(fixture.ctx, ResolveCommandInput{Name: "okt-task-implement"})
	if err != nil {
		t.Fatalf("ResolveCommand() error = %v", err)
	}
	if len(resp.Skills) != 2 {
		t.Fatalf("resolved skills = %d, want 2 from SkillRepertoire fallback: %+v", len(resp.Skills), resp.Skills)
	}
	if resp.Skills[0].Slug != "go" || resp.Skills[1].Slug != "sqlite" {
		t.Fatalf("resolved skill order = [%s %s], want [go sqlite]", resp.Skills[0].Slug, resp.Skills[1].Slug)
	}
	if !strings.Contains(resp.Markdown, "## Skills") {
		t.Fatalf("rendered markdown missing ## Skills section:\n%s", resp.Markdown)
	}
	if !strings.Contains(resp.Markdown, "**Go**") {
		t.Fatalf("rendered markdown missing repertoire skill Go:\n%s", resp.Markdown)
	}
}

// The persona-agnostic contract (command playbooks carry no persona-specific
// role prose, since the bound persona is configurable) is now an entity-sourced
// property of the bound okt-<slug>-playbook skills, asserted against the
// rendered default kit by agentruntime.TestCommandPlaybooksArePersonaAgnostic —
// the Go layer no longer carries the action prose to check here.

// TestResolveCommandWithoutCatalogsDegradesGracefully guards the degraded path:
// when the runtime is unwired (no skills/laws/personas/commands catalogs),
// ResolveCommand still resolves a registered command without error so the MCP
// harness keeps working through partial bootstraps. The command playbook is
// entity-sourced now, so with no skill catalog there is nothing to render — the
// description is empty and no persona/skills attach — but resolution must not
// fail, and an unknown command must still reject.
func TestResolveCommandWithoutCatalogsDegradesGracefully(t *testing.T) {
	fixture := newAgentFixture(t)
	resp, err := fixture.service.ResolveCommand(fixture.ctx, ResolveCommandInput{Name: "okt"})
	if err != nil {
		t.Fatalf("ResolveCommand() error = %v", err)
	}
	if resp.Name != "okt" {
		t.Fatalf("ResolveCommand.Name = %q, want okt", resp.Name)
	}
	if resp.Description != "" {
		t.Fatalf("ResolveCommand.Description = %q, want empty when the skill catalog is unwired", resp.Description)
	}
	if resp.Persona != nil {
		t.Fatalf("ResolveCommand.Persona = %+v, want nil when catalogs unwired", resp.Persona)
	}
	if len(resp.Skills) != 0 {
		t.Fatalf("ResolveCommand.Skills = %+v, want none when the skill catalog is unwired", resp.Skills)
	}
	// An unknown command still rejects even on the degraded path.
	if _, err := fixture.service.ResolveCommand(fixture.ctx, ResolveCommandInput{Name: "okt-bogus"}); err == nil {
		t.Fatal("ResolveCommand(unknown) error = nil, want validation failure even when unwired")
	}
}

func wireBindingFixtures(t *testing.T, fixture agentFixture) {
	t.Helper()
	wireBindingFixturesWithPersona(t, fixture, PersonaInfo{
		Slug:        "backend-agent",
		Name:        "Backend Agent",
		Description: "Backend-focused agent.",
		Skills:      []string{"go"},
		Laws:        []string{"project-scope-only"},
	})
}

// wireBindingFixturesWithPersona swaps the persona that wireBindingFixtures
// installs without touching the rest of the catalog wiring. Used by the
// few tests that want to vary the persona body (e.g. the Loop-step
// rendering test) while keeping the standard skills/laws/templates/
// commands stable.
func wireBindingFixturesWithPersona(t *testing.T, fixture agentFixture, persona PersonaInfo) {
	t.Helper()
	skills := []SkillInfo{
		{Slug: "go", Name: "Go", Description: "Idiomatic Go.", Body: "Go body."},
		{Slug: "sqlite", Name: "SQLite", Body: "SQLite body."},
	}
	laws := []LawInfo{
		// Body deliberately mirrors the production shape: directive paragraph
		// followed by Bad:/Good: examples. The few-shot test asserts the
		// markers are forwarded verbatim through ResolveCommand's renderer,
		// so any future compression that strips them surfaces here.
		{Slug: "template-fidelity", Name: "Template fidelity", Severity: "warning", Body: "Do not invent fields.\n\nBad: wrote `Closes #40`.\nGood: left References blank."},
		{Slug: "project-scope-only", Name: "Project scope only", Severity: "error", Body: "Never mix projects."},
	}
	templates := []TemplateSummary{
		{Slug: "pull-request", Name: "Pull Request", Default: "pr", Body: "## Before\n## After\n", Laws: []string{"template-fidelity"}},
	}
	commands := map[string]MCPCommandBinding{
		MCPCommandsGlobalKey: {Laws: []string{"template-fidelity"}},
		"okt":                {Persona: "backend-agent"},
		"okt-task-implement": {Persona: "backend-agent", Templates: []string{"pull-request"}},
		"okt-task-imagine":   {Persona: "backend-agent", LawsDisabled: []string{"template-fidelity"}},
	}
	fixture.service.SetSnapshot(snapshotWithEntities(t, skills, laws, []PersonaInfo{persona}, templates, commands))
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestNoGoCommandProseFallback is the no-Go-fallback guard (AC#2 of #603, the
// automated enforcement of #598's "no hardcoded operational prose in Go"
// goal). The okt-* command playbook (the operational ## Action prose) and the
// prompts/list one-liner are ENTITY-SOURCED: they must come solely from the
// bound okt-<slug>-playbook skill, never from a Go table/fallback. This guard
// proves Go injects NO such prose, and it is written to FAIL the moment a
// fallback is reintroduced.
//
// It bites on two reintroduction shapes:
//
//  1. Behaviourally — it resolves a registered command with the skill catalog
//     present but EMPTY (no okt-<slug>-playbook skill bound). With entities
//     absent, the only thing that could populate resp.Description or emit a
//     playbook/Action body is a Go fallback. The guard asserts both stay empty.
//     Reintroduce a Go default (e.g. `resp.Description = actionTable[name]` or
//     a `## Action` render block) and Description becomes non-empty / the body
//     appears, and this test fails.
//
//  2. Structurally — it scans the command_table.go and ResolveCommand source
//     for the removed-prose symbols (CommandActionFallback, a per-command
//     Action/Description field, a `## Action` render section). Reintroducing
//     any of them as a Go symbol trips the scan.
func TestNoGoCommandProseFallback(t *testing.T) {
	// --- Behavioural half: entities absent ⇒ no Go-injected prose. ---
	t.Run("resolution_has_no_go_fallback_with_empty_skill_catalog", func(t *testing.T) {
		fixture := newAgentFixture(t)
		// Wire personas + commands but NO skills: the skill catalog is present
		// yet empty, so no okt-<slug>-playbook body or frontmatter exists. Any
		// non-empty playbook prose now could only come from Go.
		fixture.service.SetSnapshot(snapshotWithEntities(t,
			nil, // skills: deliberately empty
			[]LawInfo{{Slug: "project-scope-only", Name: "Project scope only", Severity: "error", Body: "Never mix projects."}},
			[]PersonaInfo{{Slug: "backend-agent", Name: "Backend Agent", Body: "Backend body."}},
			nil, // templates
			map[string]MCPCommandBinding{
				MCPCommandsGlobalKey: {Laws: []string{"project-scope-only"}},
				"okt":                {Persona: "backend-agent"},
				"okt-start":          {Persona: "backend-agent"},
				"okt-task-implement": {Persona: "backend-agent"},
			},
		))

		for _, name := range []string{"okt", "okt-start", "okt-task-implement"} {
			resp, err := fixture.service.ResolveCommand(fixture.ctx, ResolveCommandInput{Name: name})
			if err != nil {
				t.Fatalf("ResolveCommand(%s) error = %v", name, err)
			}
			// The prompts/list one-liner is entity-sourced only. With no bound
			// playbook skill it MUST degrade to empty — a non-empty value means
			// a Go fallback re-injected a hardcoded description.
			if strings.TrimSpace(resp.Description) != "" {
				t.Fatalf("%s carries a description %q with an empty skill catalog — a Go fallback re-injected hardcoded prompts/list prose; descriptions must come solely from the bound okt-<slug>-playbook skill frontmatter", name, resp.Description)
			}
			// CommandDescription is the dedicated accessor the MCP adapter uses;
			// it must degrade identically.
			if got := fixture.service.CommandDescription(name); strings.TrimSpace(got) != "" {
				t.Fatalf("CommandDescription(%s) = %q with an empty skill catalog — a Go fallback re-injected hardcoded prose", name, got)
			}
			// No hardcoded `## Action` playbook section may be rendered — the
			// playbook is the bound skill body under `## Skills`, and with no
			// skills there is nothing to render.
			if strings.Contains(resp.Markdown, "## Action") {
				t.Fatalf("%s rendered a `## Action` section — the command playbook must be the entity-sourced okt-<slug>-playbook skill body under `## Skills`, never a hardcoded Go Action block:\n%s", name, resp.Markdown)
			}
			if strings.Contains(resp.Markdown, "## Skills") {
				t.Fatalf("%s rendered a `## Skills` section despite an empty skill catalog — Go injected a fallback skill/playbook body:\n%s", name, resp.Markdown)
			}
		}
	})

	// --- Structural half: removed-prose symbols stay out of the Go source. ---
	t.Run("command_source_carries_no_removed_prose_symbols", func(t *testing.T) {
		// `go test` runs with the package directory as CWD, so the command
		// source sits beside this test file.
		for _, file := range []string{"command_table.go", "service_command.go"} {
			path := filepath.Join(".", file)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v — adjust the guard if the file moved", file, err)
			}
			// Strip Go comments before scanning so the architectural doc-comments
			// that legitimately MENTION the removed prose (explaining why it is
			// gone) do not trip the guard — we only care about live code.
			code := stripGoComments(string(raw))
			banned := []struct {
				token string
				why   string
			}{
				{"CommandActionFallback", "the per-command Go Action fallback was stripped; it must not return"},
				{"CommandActionEntry", "the per-command Go Action table entry was stripped; it must not return"},
				{"\"## Action", "the hardcoded `## Action` render section was removed; the playbook is the bound skill body"},
				{"`## Action", "the hardcoded `## Action` render section was removed; the playbook is the bound skill body"},
				{"Action string", "command rows carry no Action prose field — the slug is the only column"},
				{"Action:", "command rows must not be initialised with hardcoded Action prose"},
			}
			for _, b := range banned {
				if strings.Contains(code, b.token) {
					t.Fatalf("%s contains the removed symbol/prose %q in live code — %s. The okt-* command playbook and description are entity-sourced; reintroducing Go prose breaks the migration.", file, b.token, b.why)
				}
			}
		}
	})
}

// stripGoComments removes line (`//`) and block (`/* */`) comments from Go
// source so the no-fallback guard scans live code only. It does not need to be
// a full lexer — the command source carries no string literals containing
// comment delimiters, so the simple state machine below is exact for this use.
func stripGoComments(src string) string {
	var b strings.Builder
	const (
		code = iota
		line
		block
	)
	state := code
	for i := 0; i < len(src); i++ {
		switch state {
		case code:
			if i+1 < len(src) && src[i] == '/' && src[i+1] == '/' {
				state = line
				i++
				continue
			}
			if i+1 < len(src) && src[i] == '/' && src[i+1] == '*' {
				state = block
				i++
				continue
			}
			b.WriteByte(src[i])
		case line:
			if src[i] == '\n' {
				state = code
				b.WriteByte(src[i])
			}
		case block:
			if i+1 < len(src) && src[i] == '*' && src[i+1] == '/' {
				state = code
				i++
			}
		}
	}
	return b.String()
}

// TestCreateTaskWithTemplateSlugMergesBody verifies the auto-apply behavior in
// tasks.create when the agent passes template_slug. Empty user description ⇒
// description is the template body. Non-empty description ⇒ user content
// stays first, template body is appended after a blank line.
func TestCreateTaskWithTemplateSlugMergesBody(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetSnapshot(snapshotWithTemplates(t, []TemplateSummary{
			{Slug: "user-story", Name: "Story", Default: "task", Body: "**User Story**\n\nAs a [role]..."},
		}))

	resp, err := fixture.service.CreateTask(fixture.ctx, CreateTaskInput{
		Title:        "Brand new direction",
		Description:  "intro",
		TemplateSlug: "user-story",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if resp.Task == nil {
		t.Fatal("CreateTask().Task = nil")
	}
	if !strings.HasPrefix(resp.Task.Description, "intro") {
		t.Fatalf("merged description should start with user content, got %q", resp.Task.Description)
	}
	if !strings.Contains(resp.Task.Description, "**User Story**") {
		t.Fatalf("merged description should embed template body, got %q", resp.Task.Description)
	}
}

// TestCreateTaskWithUnknownTemplateSlugFails ensures the validation error
// surfaces instead of silently degrading to no-template behavior — the agent
// passing a bad slug is a programming error, not something to paper over.
func TestCreateTaskWithUnknownTemplateSlugFails(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetSnapshot(snapshotWithTemplates(t, nil))

	_, err := fixture.service.CreateTask(fixture.ctx, CreateTaskInput{
		Title:        "Brand new",
		Description:  "x",
		TemplateSlug: "missing",
	})
	if err == nil {
		t.Fatal("CreateTask(template_slug=missing) error = nil, want not-found")
	}
}

// TestAddCommentTemplateSlugMergesBody verifies the same merge behavior for
// the comments.add path so resume/self-branch templates can pre-fill comment
// bodies without dynamic placeholder support.
func TestAddCommentTemplateSlugMergesBody(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetSnapshot(snapshotWithTemplates(t, []TemplateSummary{
			{Slug: "comment-resume", Name: "Resume", Default: "comment-resume", Body: "## What changed\n## Open questions"},
		}))

	resp, err := fixture.service.AddComment(fixture.ctx, AddCommentInput{
		TaskID:       fixture.taskA1.ID,
		Body:         "kicking off review",
		AuthorType:   "agent",
		TemplateSlug: "comment-resume",
	})
	if err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}
	if !strings.HasPrefix(resp.Comment.Body, "kicking off review") {
		t.Fatalf("comment body should start with user content, got %q", resp.Comment.Body)
	}
	if !strings.Contains(resp.Comment.Body, "## What changed") {
		t.Fatalf("comment body should embed template body, got %q", resp.Comment.Body)
	}
}

// TestCreateTaskWithTemplateSlugSkipsDuplicateScaffold pins the dedupe
// behavior in mergeUserBodyWithTemplate. When the agent fetches a template,
// fills its sections, and then passes both the filled body AND the same
// template_slug, the merge must detect the structural overlap and skip the
// append — otherwise every `## …` section ends up duplicated, once with the
// agent-filled content and once with the raw placeholder scaffold.
func TestCreateTaskWithTemplateSlugSkipsDuplicateScaffold(t *testing.T) {
	fixture := newAgentFixture(t)
	scaffold := "## Description\n\nAs a [role], I want [capability].\n\n## Acceptance criteria\n\n1.\n\n## Definition of done\n\n- [ ] tests pass"
	fixture.service.SetSnapshot(snapshotWithTemplates(t, []TemplateSummary{
			{Slug: "user-story", Name: "Story", Default: "task", Body: scaffold},
		}))

	filled := "## Description\n\nAs a developer, I want template dedupe.\n\n## Acceptance criteria\n\n1. filled scaffold is not duplicated.\n\n## Definition of done\n\n- [ ] regression test green"

	resp, err := fixture.service.CreateTask(fixture.ctx, CreateTaskInput{
		Title:        "Story",
		Description:  filled,
		TemplateSlug: "user-story",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if resp.Task == nil {
		t.Fatal("CreateTask().Task = nil")
	}
	if resp.Task.Description != filled {
		t.Fatalf("merged description should equal filled scaffold verbatim, got %q", resp.Task.Description)
	}
	if strings.Contains(resp.Task.Description, "As a [role]") {
		t.Fatalf("merged description should not embed raw scaffold placeholder, got %q", resp.Task.Description)
	}
	if strings.Count(resp.Task.Description, "## Description") != 1 {
		t.Fatalf("merged description should not duplicate the ## Description heading, got %q", resp.Task.Description)
	}
}

// TestAddCommentTemplateSlugSkipsDuplicateScaffold mirrors the task surface
// test on the comment path — same dedupe contract, same regression risk
// because both surfaces flow through mergeUserBodyWithTemplate.
func TestAddCommentTemplateSlugSkipsDuplicateScaffold(t *testing.T) {
	fixture := newAgentFixture(t)
	scaffold := "## What changed\n\n- [ ] entry\n\n## Open questions\n\n- [ ] question"
	fixture.service.SetSnapshot(snapshotWithTemplates(t, []TemplateSummary{
			{Slug: "comment-resume", Name: "Resume", Default: "comment-resume", Body: scaffold},
		}))

	filled := "## What changed\n\n- migrated tests.\n\n## Open questions\n\n- none."

	resp, err := fixture.service.AddComment(fixture.ctx, AddCommentInput{
		TaskID:       fixture.taskA1.ID,
		Body:         filled,
		AuthorType:   "agent",
		TemplateSlug: "comment-resume",
	})
	if err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}
	if resp.Comment.Body != filled {
		t.Fatalf("comment body should equal filled scaffold verbatim, got %q", resp.Comment.Body)
	}
	if strings.Count(resp.Comment.Body, "## What changed") != 1 {
		t.Fatalf("comment body should not duplicate the ## What changed heading, got %q", resp.Comment.Body)
	}
}
