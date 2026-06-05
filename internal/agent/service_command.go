package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"omakiten/internal/domain"
)

// ResolveCommand assembles the persona/skills/laws/templates package bound to
// `name` and returns it both structured and rendered as a single markdown
// message ready for an MCP PromptMessage. The resolution follows the rules
// documented in `.docs/configuration-guide/command-bindings.md`:
//
//   - effective laws = global ∪ persona.laws ∪ command.laws ∪ templates[].laws,
//     minus command.laws_disabled, deduped, in first-seen order;
//   - persona is the one declared on the command spec (no default);
//   - skills come from the command subset, then legacy persona skills, then
//     the persona's schema-v2 skill repertoire;
//   - templates are the slugs declared on the command spec;
//   - the command playbook is entity-sourced: the prompts/list description comes
//     from the bound okt-<slug>-playbook skill's frontmatter, and its body
//     renders among the skills — Go carries no Action/Description prose.
//
// Missing catalogs degrade gracefully — an unwired runtime still resolves a
// registered command (empty description, no persona/skills) so the MCP harness
// keeps working through the upgrade; an unknown command still rejects.
func (s *Service) ResolveCommand(_ context.Context, input ResolveCommandInput) (ResolveCommandResponse, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return ResolveCommandResponse{}, domain.NewError(domain.ErrValidation, "command name is required", nil)
	}
	if !isKnownCommand(name) {
		return ResolveCommandResponse{}, domain.NewError(domain.ErrValidation, "unknown MCP command", map[string]any{"name": name})
	}

	resp := ResolveCommandResponse{Name: name}

	commands := s.loadCommandCatalog()
	personas := s.loadPersonaCatalog()
	skills := s.loadSkillCatalog()
	laws := s.loadLawCatalog()
	templates := s.loadTemplateCatalogForCommand()

	// The prompts/list one-liner is entity-sourced: it is the frontmatter
	// `description` of the bound okt-<slug>-playbook skill, not Go prose. An
	// unwired runtime (no skill catalog) degrades to an empty description.
	if pb, ok := skills[playbookSlugForCommand(name)]; ok {
		resp.Description = pb.Description
	}

	spec := commands[name]
	globalSpec := commands[MCPCommandsGlobalKey]

	if spec.Persona != "" {
		if persona, ok := personas[spec.Persona]; ok {
			info := persona
			resp.Persona = &info
			// Command-level skills (schema v2) win over the persona's full
			// repertoire: a themed command ships only the minimal subset it
			// declares. Commands that omit command-level skills fall back to
			// the persona's directly-wired Skills (v1), then to its
			// SkillRepertoire (v2) so a schema-v2 persona whose pool lives in
			// SkillRepertoire still renders its skills.
			slugs := spec.Skills
			if len(slugs) == 0 {
				slugs = persona.Skills
			}
			if len(slugs) == 0 {
				slugs = persona.SkillRepertoire
			}
			resp.Skills = pickSkills(slugs, skills)
		}
	}

	for _, slug := range spec.Templates {
		if t, ok := templates[slug]; ok {
			resp.Templates = append(resp.Templates, t)
		}
	}

	resp.Laws = effectiveLaws(globalSpec, spec, resp.Persona, resp.Templates, laws)
	if s.snapshot != nil {
		resp.AgentOutputLanguage = s.snapshot.AgentOutputLanguage()
	}
	resp.Markdown = renderCommandMarkdown(resp)
	return resp, nil
}

// CommandDescription returns the entity-sourced prompts/list one-liner for a
// command: the frontmatter `description` of its bound okt-<slug>-playbook skill.
// It returns the empty string for an unknown command or an unwired runtime (no
// skill catalog / no matching playbook skill) — callers treat empty as "no
// description available" rather than an error.
func (s *Service) CommandDescription(name string) string {
	if !isKnownCommand(name) {
		return ""
	}
	if pb, ok := s.loadSkillCatalog()[playbookSlugForCommand(name)]; ok {
		return pb.Description
	}
	return ""
}

func (s *Service) loadCommandCatalog() map[string]MCPCommandBinding {
	if s.commandCatalog == nil {
		return map[string]MCPCommandBinding{}
	}
	return s.commandCatalog()
}

func (s *Service) loadPersonaCatalog() map[string]PersonaInfo {
	out := map[string]PersonaInfo{}
	if s.personaCatalog == nil {
		return out
	}
	for _, p := range s.personaCatalog() {
		out[p.Slug] = p
	}
	return out
}

func (s *Service) loadSkillCatalog() map[string]SkillInfo {
	out := map[string]SkillInfo{}
	if s.skillCatalog == nil {
		return out
	}
	for _, sk := range s.skillCatalog() {
		out[sk.Slug] = sk
	}
	return out
}

func (s *Service) loadLawCatalog() map[string]LawInfo {
	out := map[string]LawInfo{}
	if s.lawCatalog == nil {
		return out
	}
	for _, l := range s.lawCatalog() {
		out[l.Slug] = l
	}
	return out
}

// loadTemplateCatalogForCommand reuses the same template snapshot the
// templates.list/show endpoints expose. Bodies are kept so the resolved
// command can ship the scaffold inline; project-scoped templates are
// surfaced verbatim — the resolver does not pick a winner here, the
// command spec already decided which slugs to bind.
func (s *Service) loadTemplateCatalogForCommand() map[string]TemplateInfo {
	out := map[string]TemplateInfo{}
	if s.templateCatalog == nil {
		return out
	}
	for _, t := range s.templateCatalog() {
		out[t.Slug] = TemplateInfo{
			Slug:        t.Slug,
			Name:        t.Name,
			Description: t.Description,
			Default:     t.Default,
			Project:     t.Project,
			Laws:        append([]string(nil), t.Laws...),
			Body:        t.Body,
		}
	}
	return out
}

// pickSkills resolves an ordered list of skill slugs against the skill catalog,
// preserving the order the caller declared them in and silently dropping slugs
// the catalog does not know. Callers pass either the command's declared skill
// subset (schema v2) or the persona's full repertoire (fallback).
func pickSkills(slugs []string, skills map[string]SkillInfo) []SkillInfo {
	if len(slugs) == 0 || len(skills) == 0 {
		return nil
	}
	out := make([]SkillInfo, 0, len(slugs))
	for _, slug := range slugs {
		if sk, ok := skills[slug]; ok {
			out = append(out, sk)
		}
	}
	return out
}

// effectiveLaws computes the deduped union of global, persona, command and
// template-bound law slugs, then subtracts laws_disabled. Each surviving slug
// is resolved against the law catalog so the prompt ships the law body, not
// just the name.
func effectiveLaws(globalSpec, commandSpec MCPCommandBinding, persona *PersonaInfo, templates []TemplateInfo, laws map[string]LawInfo) []LawInfo {
	disabled := map[string]struct{}{}
	for _, slug := range commandSpec.LawsDisabled {
		disabled[slug] = struct{}{}
	}

	seen := map[string]struct{}{}
	out := []LawInfo{}
	add := func(slug string) {
		if slug == "" {
			return
		}
		if _, dup := seen[slug]; dup {
			return
		}
		if _, off := disabled[slug]; off {
			return
		}
		seen[slug] = struct{}{}
		if law, ok := laws[slug]; ok {
			out = append(out, law)
		}
	}

	for _, slug := range globalSpec.Laws {
		add(slug)
	}
	if persona != nil {
		for _, slug := range persona.Laws {
			add(slug)
		}
	}
	for _, slug := range commandSpec.Laws {
		add(slug)
	}
	for _, t := range templates {
		for _, slug := range t.Laws {
			add(slug)
		}
	}
	return out
}

// renderCommandMarkdown produces the single PromptMessage body the MCP layer
// returns. Sections are kept compact and ordered (persona → skills → laws →
// templates) so the agent can scan them top-down without reordering.
//
// There is no `## Action` section anymore: the command's operational playbook
// is ENTITY-SOURCED. Each command binds an `okt-<slug>-playbook` skill, so the
// playbook body arrives in the `## Skills` section like any other skill body —
// the Go layer no longer carries a duplicate copy of the prose.
//
// The prompt name and description are NOT echoed in the body — they ship via
// `prompts/list` metadata in the MCP protocol, which every aware client
// surfaces before calling `prompts/get`. Emitting them again here would just
// duplicate bytes the agent already has.
//
// Skills render as bullet-with-body under `## Skills` — one bullet per skill
// in configured (persona-wiring) order, the bound playbook skill among them.
// Each bullet carries the skill body (the procedural payload) when present;
// skills without a body fall back to their description, and skills with neither
// render as a bare name bullet.
//
// Laws render under `## Laws` (no count parenthetical) — the number is
// decorative; the agent does not branch on it.
//
// Templates render as JIT metadata: slug, optional name (only when it diverges
// from the title-case of the slug), optional default kind, optional
// description. The fetch hint (`templates.show <slug>`) is NOT emitted as a
// trailing footer; instead, every templates-bound command must surface the
// hint via its action text or its persona body. This is enforced by
// `TestTemplateBoundCommandsCarryFetchHint`.
func renderCommandMarkdown(resp ResolveCommandResponse) string {
	var b strings.Builder
	sectionStarted := false
	openSection := func(heading string) {
		if sectionStarted {
			b.WriteString("\n")
		}
		b.WriteString(heading)
		b.WriteString("\n")
		sectionStarted = true
	}

	if resp.Persona != nil {
		openSection(fmt.Sprintf("## Persona — %s", resp.Persona.Name))
		if resp.Persona.Description != "" {
			fmt.Fprintf(&b, "%s\n", resp.Persona.Description)
		}
		if body := strings.TrimSpace(resp.Persona.Body); body != "" {
			fmt.Fprintf(&b, "\n%s\n", body)
		}
	}

	if len(resp.Skills) > 0 {
		openSection("## Skills")
		for _, sk := range resp.Skills {
			label := sk.Name
			if label == "" {
				label = sk.Slug
			}
			// Body is the procedural payload; description is the fallback when
			// a skill carries no body. Skills with neither render as a bare
			// name bullet so configured order and presence stay visible.
			detail := strings.TrimSpace(sk.Body)
			if detail == "" {
				detail = strings.TrimSpace(sk.Description)
			}
			if detail == "" {
				fmt.Fprintf(&b, "- **%s**\n", label)
				continue
			}
			renderBulletWithBody(&b, label, detail)
		}
	}

	if len(resp.Laws) > 0 {
		openSection("## Laws")
		for _, law := range resp.Laws {
			label := law.Name
			if label == "" {
				label = law.Slug
			}
			body := strings.TrimSpace(law.Body)
			renderBulletWithBody(&b, fmt.Sprintf("[%s] %s", law.Severity, label), body)
		}
	}

	if len(resp.Templates) > 0 {
		openSection("## Templates")
		for _, t := range resp.Templates {
			line := fmt.Sprintf("- **%s**", t.Slug)
			if t.Name != "" && !templateNameEchoesSlug(t.Name, t.Slug) {
				line += fmt.Sprintf(" — %s", t.Name)
			}
			if t.Default != "" {
				line += fmt.Sprintf(" (default: %s)", t.Default)
			}
			if desc := strings.TrimSpace(t.Description); desc != "" {
				line += fmt.Sprintf(" — %s", desc)
			}
			fmt.Fprintln(&b, line)
		}
	}

	// Trailing output-language directive: byte-stable per session so the
	// Anthropic prompt cache hit rate stays high. Skipped entirely when
	// the user has not configured config.languages.agent_output — no
	// blank line, no marker, no observable change to existing prompts.
	if lang := strings.TrimSpace(resp.AgentOutputLanguage); lang != "" {
		fmt.Fprintf(&b, "\n**Output language:** %s\n", lang)
	}
	return b.String()
}

// renderBulletWithBody writes one Markdown list item whose detail may span
// multiple lines. The bolded label leads the bullet (`- **<label>** — <head>`)
// and every continuation line is indented two spaces so the body stays
// visually nested under the bullet — blank lines pass through verbatim so
// paragraph breaks inside the body survive. Shared by the `## Skills` and
// `## Laws` sections, which differ only in how the label is composed (a skill
// name vs `[severity] law-name`); the multi-line indent handling is identical,
// so it lives here once. detail is assumed non-empty.
func renderBulletWithBody(b *strings.Builder, label, detail string) {
	if idx := strings.Index(detail, "\n"); idx >= 0 {
		head := detail[:idx]
		tail := detail[idx+1:]
		fmt.Fprintf(b, "- **%s** — %s\n", label, head)
		for _, line := range strings.Split(tail, "\n") {
			if line == "" {
				fmt.Fprintln(b)
				continue
			}
			fmt.Fprintf(b, "  %s\n", line)
		}
		return
	}
	fmt.Fprintf(b, "- **%s** — %s\n", label, detail)
}

// templateNameEchoesSlug reports whether the human-readable template name is
// just the title-case of the slug (with hyphens turned into spaces). When
// true, the renderer drops the name from the bound-template line because it
// carries no information beyond the slug. The slug stays — the agent uses it
// to call `templates.show`.
//
// "config-orientation" / "Config Orientation" → true (drop name)
// "pull-request"       / "Pull Request"       → true (drop name)
// "pr"                 / "Pull Request"       → false (keep name)
// "comment-resume"     / "Resume comment"     → false (keep name)
func templateNameEchoesSlug(name, slug string) bool {
	if name == "" || slug == "" {
		return false
	}
	parts := strings.Split(slug, "-")
	titled := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		titled = append(titled, strings.ToUpper(p[:1])+p[1:])
	}
	return strings.EqualFold(strings.Join(titled, " "), name)
}

// SortedCommandNames is exposed for tests that want a stable iteration order.
// It is a thin wrapper around CommandNames; both are in the agent layer so
// the MCP adapter does not have to maintain its own list.
func SortedCommandNames() []string {
	out := append([]string(nil), CommandNames()...)
	sort.Strings(out)
	return out
}
