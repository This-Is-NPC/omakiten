package operation

import (
	"context"
	"encoding/json"
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
//   - skills come from the command subset, then the persona's current skill
//     repertoire;
//   - templates are the slugs declared on the command spec;
//   - the command playbook is entity-sourced: the prompts/list description comes
//     from the bound okt-<slug>-playbook skill's frontmatter, and its body
//     renders among the skills — Go carries no Action/Description prose.
//
// Missing catalogs degrade gracefully — an unwired runtime still resolves a
// registered command (empty description, no persona/skills); an unknown
// command still rejects.
func (s *Service) ResolveCommand(_ context.Context, input ResolveCommandInput) (ResolveCommandResponse, error) {
	if err := s.allow("command.resolve"); err != nil {
		return ResolveCommandResponse{}, err
	}
	name := strings.TrimSpace(input.Name)
	commands := s.loadCommandCatalog()
	personas := s.loadPersonaCatalog()
	skills := s.loadSkillCatalog()
	laws := s.loadLawCatalog()
	templates := s.loadTemplateCatalogForCommand()
	outputLanguage := ""
	if s.snapshot != nil {
		outputLanguage = s.snapshot.AgentOutputLanguage()
	}
	return resolveCommandFromCatalog(name, invocationArgs(input.Arguments), commands, personas, skills, laws, templates, outputLanguage)
}

// ListCommands returns every agent-callable okt-* command slug with its
// entity-sourced description. MCP tool commands.list is this gated
// discovery surface; Adapter.Prompts() (prompts/list) stays ungated so
// a human slash-command picker still works when the tool is denied.
func (s *Service) ListCommands(_ context.Context) (ListCommandsResponse, error) {
	if err := s.allow("command.list"); err != nil {
		return ListCommandsResponse{}, err
	}
	names := CommandNames()
	out := make([]CommandListEntry, 0, len(names))
	for _, name := range names {
		out = append(out, CommandListEntry{Name: name, Description: s.CommandDescription(name)})
	}
	return ListCommandsResponse{Commands: out}, nil
}

// ResolveCommandFromCatalog resolves a command against an unsaved candidate
// catalog. Studio uses this entry point so prompt preview and MCP runtime
// resolution share one composition and rendering implementation.
func ResolveCommandFromCatalog(name string, commands map[string]MCPCommandBinding, personas map[string]PersonaInfo, skills map[string]SkillInfo, laws map[string]LawInfo, templates map[string]TemplateInfo, outputLanguage string) (ResolveCommandResponse, error) {
	return resolveCommandFromCatalog(strings.TrimSpace(name), nil, commands, personas, skills, laws, templates, outputLanguage)
}

func resolveCommandFromCatalog(name string, args []InvocationArg, commands map[string]MCPCommandBinding, personas map[string]PersonaInfo, skills map[string]SkillInfo, laws map[string]LawInfo, templates map[string]TemplateInfo, outputLanguage string) (ResolveCommandResponse, error) {
	if name == "" {
		return ResolveCommandResponse{}, domain.NewError(domain.ErrValidation, "command name is required", nil)
	}
	if !isKnownCommand(name) {
		return ResolveCommandResponse{}, domain.NewError(domain.ErrValidation, "unknown MCP command", map[string]any{"name": name})
	}

	resp := ResolveCommandResponse{Name: name, InvocationArgs: args}

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
			// Command-level skills win over the persona's full repertoire: a
			// themed command ships only the minimal subset it declares.
			// Commands that omit command-level skills use the persona's
			// current skill repertoire.
			slugs := spec.Skills
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
	resp.AgentOutputLanguage = outputLanguage
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

func invocationArgs(args map[string]any) []InvocationArg {
	if len(args) == 0 {
		return nil
	}
	keys := make([]string, 0, len(args))
	values := make(map[string]any, len(args))
	for key, value := range args {
		key = strings.TrimSpace(key)
		if key != "" {
			keys = append(keys, key)
			values[key] = value
		}
	}
	sort.Strings(keys)
	out := make([]InvocationArg, 0, len(keys))
	for _, key := range keys {
		out = append(out, InvocationArg{Name: key, Value: formatInvocationArg(values[key])})
	}
	return out
}

func formatInvocationArg(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
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

	slugs := append([]string(nil), globalSpec.Laws...)
	if persona != nil {
		slugs = append(slugs, persona.Laws...)
	}
	slugs = append(slugs, commandSpec.Laws...)
	for _, t := range templates {
		slugs = append(slugs, t.Laws...)
	}
	return resolveEffectiveLaws(slugs, disabled, laws)
}

func resolveEffectiveLaws(slugs []string, disabled map[string]struct{}, laws map[string]LawInfo) []LawInfo {
	seen := map[string]struct{}{}
	out := []LawInfo{}
	for _, slug := range slugs {
		if slug == "" {
			continue
		}
		if _, dup := seen[slug]; dup {
			continue
		}
		if _, off := disabled[slug]; off {
			continue
		}
		seen[slug] = struct{}{}
		if law, ok := laws[slug]; ok {
			out = append(out, law)
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
	r := markdownRenderer{}
	r.writePersona(resp.Persona)
	r.writeInvocationArgs(resp.InvocationArgs)
	r.writeSkills(resp.Skills)
	r.writeLaws(resp.Laws)
	r.writeTemplates(resp.Templates)
	r.writeOutputLanguage(resp.AgentOutputLanguage)
	return r.b.String()
}

type markdownRenderer struct {
	b              strings.Builder
	sectionStarted bool
}

func (r *markdownRenderer) openSection(heading string) {
	if r.sectionStarted {
		r.b.WriteString("\n")
	}
	r.b.WriteString(heading)
	r.b.WriteString("\n")
	r.sectionStarted = true
}

func (r *markdownRenderer) writePersona(persona *PersonaInfo) {
	if persona == nil {
		return
	}
	r.openSection(fmt.Sprintf("## Persona — %s", persona.Name))
	if persona.Description != "" {
		fmt.Fprintf(&r.b, "%s\n", persona.Description)
	}
	if body := strings.TrimSpace(persona.Body); body != "" {
		fmt.Fprintf(&r.b, "\n%s\n", body)
	}
}

func (r *markdownRenderer) writeInvocationArgs(args []InvocationArg) {
	if len(args) == 0 {
		return
	}
	r.openSection("## Invocation Args")
	for _, arg := range args {
		fmt.Fprintf(&r.b, "- `%s`: %s\n", arg.Name, arg.Value)
	}
}

func (r *markdownRenderer) writeSkills(skills []SkillInfo) {
	if len(skills) == 0 {
		return
	}
	r.openSection("## Skills")
	for _, skill := range skills {
		label := skill.Name
		if label == "" {
			label = skill.Slug
		}
		detail := strings.TrimSpace(skill.Body)
		if detail == "" {
			detail = strings.TrimSpace(skill.Description)
		}
		if detail == "" {
			fmt.Fprintf(&r.b, "- **%s**\n", label)
			continue
		}
		renderBulletWithBody(&r.b, label, detail)
	}
}

func (r *markdownRenderer) writeLaws(laws []LawInfo) {
	if len(laws) == 0 {
		return
	}
	r.openSection("## Laws")
	for _, law := range laws {
		label := law.Name
		if label == "" {
			label = law.Slug
		}
		renderBulletWithBody(&r.b, fmt.Sprintf("[%s] %s", law.Severity, label), strings.TrimSpace(law.Body))
	}
}

func (r *markdownRenderer) writeTemplates(templates []TemplateInfo) {
	if len(templates) == 0 {
		return
	}
	r.openSection("## Templates")
	for _, template := range templates {
		line := fmt.Sprintf("- **%s**", template.Slug)
		if template.Name != "" && !templateNameEchoesSlug(template.Name, template.Slug) {
			line += fmt.Sprintf(" — %s", template.Name)
		}
		if template.Default != "" {
			line += fmt.Sprintf(" (default: %s)", template.Default)
		}
		if desc := strings.TrimSpace(template.Description); desc != "" {
			line += fmt.Sprintf(" — %s", desc)
		}
		fmt.Fprintln(&r.b, line)
	}
}

func (r *markdownRenderer) writeOutputLanguage(language string) {
	if lang := strings.TrimSpace(language); lang != "" {
		fmt.Fprintf(&r.b, "\n**Output language:** %s\n", lang)
	}
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
