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
	if !strings.Contains(resp.Markdown, "## Laws (2)") {
		t.Fatalf("Markdown should headline the law count, got:\n%s", resp.Markdown)
	}
	if !strings.Contains(resp.Markdown, "Pull Request") {
		t.Fatalf("Markdown should embed the pull-request template body, got:\n%s", resp.Markdown)
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
// template metadata (slug, name, default kind, description) and a pointer to
// `templates.show`, but it must NOT embed the template body. Embedding the
// body would defeat the entire point of JIT — the body is large and the agent
// only needs it at the moment of materialization.
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
	if !strings.Contains(resp.Markdown, "templates.show") {
		t.Fatal("Markdown missing templates.show pointer — JIT contract broken")
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
	wireBindingFixtures(t, fixture)
	const marker = "PERSONA_BODY_MARKER_xyz"
	fixture.service.SetPersonaCatalog(func() []PersonaInfo {
		return []PersonaInfo{
			{
				Slug:        "backend-agent",
				Name:        "Backend Agent",
				Description: "Backend-focused agent.",
				Body:        "Loop step 1.\n" + marker + "\nLoop step 3.",
				Skills:      []string{"go"},
				Laws:        []string{"project-scope-only"},
			},
		}
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
	fixture.service.SetSkillCatalog(func() []SkillInfo {
		return []SkillInfo{
			{Slug: "go", Name: "Go", Description: "Idiomatic Go.", Body: "Go body."},
			{Slug: "sqlite", Name: "SQLite", Body: "SQLite body."},
		}
	})
	fixture.service.SetLawCatalog(func() []LawInfo {
		return []LawInfo{
			// Body deliberately mirrors the production shape: directive paragraph
			// followed by Bad:/Good: examples. The few-shot test asserts the
			// markers are forwarded verbatim through ResolveCommand's renderer,
			// so any future compression that strips them surfaces here.
			{Slug: "template-fidelity", Name: "Template fidelity", Severity: "warning", Body: "Do not invent fields.\n\nBad: wrote `Closes #40`.\nGood: left References blank."},
			{Slug: "project-scope-only", Name: "Project scope only", Severity: "error", Body: "Never mix projects."},
		}
	})
	fixture.service.SetPersonaCatalog(func() []PersonaInfo {
		return []PersonaInfo{
			{
				Slug:        "backend-agent",
				Name:        "Backend Agent",
				Description: "Backend-focused agent.",
				Body:        "",
				Skills:      []string{"go"},
				Laws:        []string{"project-scope-only"},
			},
		}
	})
	fixture.service.SetTemplateCatalog(func() []TemplateSummary {
		return []TemplateSummary{
			{Slug: "pull-request", Name: "Pull Request", Default: "pr", Body: "## Before\n## After\n", Laws: []string{"template-fidelity"}},
		}
	})
	fixture.service.SetCommandCatalog(func() map[string]MCPCommandBinding {
		return map[string]MCPCommandBinding{
			MCPCommandsGlobalKey: {Laws: []string{"template-fidelity"}},
			"okt": {
				Persona: "backend-agent",
			},
			"okt-implement": {
				Persona:   "backend-agent",
				Templates: []string{"pull-request"},
			},
			"okt-imagine": {
				Persona:      "backend-agent",
				LawsDisabled: []string{"template-fidelity"},
			},
		}
	})
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
	fixture.service.SetTemplateCatalog(func() []TemplateSummary {
		return []TemplateSummary{
			{Slug: "user-story", Name: "Story", Default: "task", Body: "**User Story**\n\nAs a [role]..."},
		}
	})

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
	fixture.service.SetTemplateCatalog(func() []TemplateSummary { return nil })

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
	fixture.service.SetTemplateCatalog(func() []TemplateSummary {
		return []TemplateSummary{
			{Slug: "comment-resume", Name: "Resume", Default: "comment-resume", Body: "## What changed\n## Open questions"},
		}
	})

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
