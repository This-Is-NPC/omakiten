package agent

import (
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

	resp, err := fixture.service.ResolveCommand(fixture.ctx, ResolveCommandInput{Name: "okt-implement"})
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

	resp, err := fixture.service.ResolveCommand(fixture.ctx, ResolveCommandInput{Name: "okt-imagine"})
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

// TestResolveCommandRestHandoffsPresent guards the REST-style hypermedia
// pattern: every command's action text must explicitly point at the next
// command in the cycle. Without this, the agent has no in-prompt hint about
// where to go after the current step and the workflow becomes guesswork.
func TestResolveCommandRestHandoffsPresent(t *testing.T) {
	expectedHandoffs := map[string][]string{
		"okt":           {"okt-resume", "okt-imagine"},
		"okt-imagine":   {"okt-create"},
		"okt-create":    {"comment-selfbranch"},
		"okt-resume":    {"okt-continue"},
		"okt-continue":  {"okt-implement"},
		"okt-implement": {"comment-resume"},
		"okt-document":  {"okt-create"},
		"okt-config":    {"templates.show", "config-orientation", "okt-implement"},
		"okt-commit":    {"git push"},
		"okt-review":    {"okt-implement"},
		"okt-check":     {"okt-implement"},
	}
	for name, hints := range expectedHandoffs {
		text := CommandActionFallback(name)
		if text == "" {
			t.Fatalf("CommandActionFallback(%s) is empty", name)
		}
		for _, hint := range hints {
			if !strings.Contains(text, hint) {
				t.Fatalf("action text for %s missing handoff %q:\n%s", name, hint, text)
			}
		}
	}
}

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

	resp, err := fixture.service.ResolveCommand(fixture.ctx, ResolveCommandInput{Name: "okt-implement"})
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

	resp, err := fixture.service.ResolveCommand(fixture.ctx, ResolveCommandInput{Name: "okt-implement"})
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
		Action: "do the thing",
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
	resp, err := fixture.service.ResolveCommand(fixture.ctx, ResolveCommandInput{Name: "okt-implement"})
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

	resp, err := fixture.service.ResolveCommand(fixture.ctx, ResolveCommandInput{Name: "okt-implement"})
	if err != nil {
		t.Fatalf("ResolveCommand() error = %v", err)
	}
	if !strings.Contains(resp.Markdown, marker) {
		t.Fatalf("persona body marker %q missing from rendered markdown:\n%s", marker, resp.Markdown)
	}
}

// TestCommandActionsArePersonaAgnostic guards the architectural separation
// between command action text and persona body across every `okt-*` prompt.
// Every command's `mcp_commands.<cmd>.persona` is configurable, so no action
// text may carry persona-specific role prose ("Take the role of an X",
// persona slug or name). Role-specific flow lives in the persona body, not
// the action text — swapping the bound persona must change the prompt
// uniformly without leaving hardcoded role instructions behind.
func TestCommandActionsArePersonaAgnostic(t *testing.T) {
	leakedPhrases := []string{
		"Take the role",
		"engineer",
		"product owner",
		"documentation curator",
		"honoring every law",
	}
	for _, name := range CommandNames() {
		action := CommandActionFallback(name)
		if action == "" {
			t.Fatalf("CommandActionFallback(%s) is empty", name)
		}
		lower := strings.ToLower(action)
		for _, phrase := range leakedPhrases {
			if strings.Contains(lower, strings.ToLower(phrase)) {
				t.Fatalf("%s action leaks persona-specific phrase %q — role prose belongs in the persona body, not the action text:\n%s", name, phrase, action)
			}
		}
	}
	// Pin okt-implement specifics: bootstrap tool + handoff marker must remain.
	implementAction := CommandActionFallback("okt-implement")
	for _, want := range []string{"tasks.continue", "comment-resume"} {
		if !strings.Contains(implementAction, want) {
			t.Fatalf("okt-implement action missing required marker %q:\n%s", want, implementAction)
		}
	}
}

// TestResolveCommandWithoutCatalogsReturnsAction guards the degraded path:
// when the runtime is unwired (no skills/laws/personas/commands catalogs),
// ResolveCommand still surfaces the canonical action text so the MCP harness
// keeps working through partial bootstraps.
func TestResolveCommandWithoutCatalogsReturnsAction(t *testing.T) {
	fixture := newAgentFixture(t)
	resp, err := fixture.service.ResolveCommand(fixture.ctx, ResolveCommandInput{Name: "okt"})
	if err != nil {
		t.Fatalf("ResolveCommand() error = %v", err)
	}
	if resp.Action == "" {
		t.Fatal("ResolveCommand.Action empty, want fallback action text")
	}
	if resp.Persona != nil {
		t.Fatalf("ResolveCommand.Persona = %+v, want nil when catalogs unwired", resp.Persona)
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
		"okt-implement":      {Persona: "backend-agent", Templates: []string{"pull-request"}},
		"okt-imagine":        {Persona: "backend-agent", LawsDisabled: []string{"template-fidelity"}},
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
