package operation

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

// ResolveCommand composes the configured persona, skills, laws and templates.
// It returns structured context and the same Markdown used by Studio previews.
func (s *Service) ResolveCommand(_ context.Context, input contract.ResolveCommandInput) (contract.ResolveCommandResponse, error) {
	if err := s.allow("command.resolve"); err != nil {
		return contract.ResolveCommandResponse{}, err
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

// ListCommands returns workflow commands with their skill descriptions.
func (s *Service) ListCommands(_ context.Context) (contract.ListCommandsResponse, error) {
	if err := s.allow("command.list"); err != nil {
		return contract.ListCommandsResponse{}, err
	}
	commands := s.loadCommandCatalog()
	skills := commandSkills(s.loadSkillCatalog())
	names := make([]string, 0, len(commands))
	for name := range commands {
		if name != CommandsGlobalKey {
			if _, ok := skills[name]; ok {
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	out := make([]contract.CommandListEntry, 0, len(names))
	for _, name := range names {
		out = append(out, contract.CommandListEntry{Name: name, Description: skills[name].Description})
	}
	return contract.ListCommandsResponse{Commands: out}, nil
}

// ResolveCommandFromCatalog previews a command against an unsaved catalog.
func ResolveCommandFromCatalog(name string, commands map[string]contract.CommandBinding, personas map[string]contract.PersonaInfo, skills map[string]contract.SkillInfo, laws map[string]contract.LawInfo, templates map[string]contract.TemplateInfo, outputLanguage string) (contract.ResolveCommandResponse, error) {
	return resolveCommandFromCatalog(strings.TrimSpace(name), nil, commands, personas, skills, laws, templates, outputLanguage)
}

func resolveCommandFromCatalog(name string, args []contract.InvocationArg, commands map[string]contract.CommandBinding, personas map[string]contract.PersonaInfo, skills map[string]contract.SkillInfo, laws map[string]contract.LawInfo, templates map[string]contract.TemplateInfo, outputLanguage string) (contract.ResolveCommandResponse, error) {
	if name == "" {
		return contract.ResolveCommandResponse{}, domain.NewError(domain.ErrValidation, "command name is required", nil)
	}
	commandSkills := commandSkills(skills)
	root, exists := commandSkills[name]
	if !exists {
		return contract.ResolveCommandResponse{}, domain.NewError(domain.ErrValidation, "unknown agent command", map[string]any{"name": name})
	}
	if _, bound := commands[name]; !bound {
		return contract.ResolveCommandResponse{}, domain.NewError(domain.ErrValidation, "unbound agent command", map[string]any{"name": name})
	}
	if err := validateInvocation(root.Command.Parameters, args); err != nil {
		return contract.ResolveCommandResponse{}, err
	}
	resp := composeCommand(name, args, commands, personas, skills, laws, templates, outputLanguage, root)
	for _, ref := range root.Command.Next {
		child := commandSkills[ref.Name]
		if child.Command == nil {
			return contract.ResolveCommandResponse{}, domain.NewError(domain.ErrValidation, "unknown related command", map[string]any{"name": ref.Name})
		}
		if _, bound := commands[ref.Name]; !bound {
			return contract.ResolveCommandResponse{}, domain.NewError(domain.ErrValidation, "unbound related command", map[string]any{"name": ref.Name})
		}
		if ref.Context != "full" && ref.Context != "bare" {
			return contract.ResolveCommandResponse{}, domain.NewError(domain.ErrValidation, "invalid related command context", map[string]any{"name": ref.Name, "context": ref.Context})
		}
		related := contract.RelatedCommand{Name: ref.Name, Description: child.Description, Context: ref.Context, When: ref.When}
		if ref.Context == "full" {
			related.Markdown = composeCommand(ref.Name, nil, commands, personas, skills, laws, templates, outputLanguage, child).Markdown
		}
		resp.Related = append(resp.Related, related)
	}
	resp.Markdown = renderCommandMarkdown(resp)
	return resp, nil
}

func commandSkills(skills map[string]contract.SkillInfo) map[string]contract.SkillInfo {
	out := make(map[string]contract.SkillInfo)
	for _, skill := range skills {
		if skill.Command != nil {
			out[skill.Command.Name] = skill
		}
	}
	return out
}

func composeCommand(name string, args []contract.InvocationArg, commands map[string]contract.CommandBinding, personas map[string]contract.PersonaInfo, skills map[string]contract.SkillInfo, laws map[string]contract.LawInfo, templates map[string]contract.TemplateInfo, outputLanguage string, root contract.SkillInfo) contract.ResolveCommandResponse {
	resp := contract.ResolveCommandResponse{Name: name, Description: root.Description, InvocationArgs: args}

	spec := commands[name]
	globalSpec := commands[CommandsGlobalKey]

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
	return resp
}

func validateInvocation(parameters []contract.CommandParameter, args []contract.InvocationArg) error {
	provided := make(map[string]string, len(args))
	for _, arg := range args {
		provided[arg.Name] = arg.Value
	}
	for _, parameter := range parameters {
		value, ok := provided[parameter.Name]
		if !ok {
			if parameter.Required {
				return domain.NewError(domain.ErrValidation, "missing command parameter", map[string]any{"name": parameter.Name})
			}
			continue
		}
		if err := validateParameterValue(parameter, value); err != nil {
			return err
		}
		delete(provided, parameter.Name)
	}
	if len(parameters) > 0 && len(provided) > 0 {
		return domain.NewError(domain.ErrValidation, "unknown command parameter", map[string]any{"name": sortedKeys(provided)[0]})
	}
	return nil
}

func validateParameterValue(parameter contract.CommandParameter, value string) error {
	var decoded any
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return domain.NewError(domain.ErrValidation, "invalid command parameter", map[string]any{"name": parameter.Name})
	}
	if commandParameterTypeMatches(parameter.Type, decoded) {
		return nil
	}
	return domain.NewError(domain.ErrValidation, "command parameter has wrong type", map[string]any{"name": parameter.Name, "type": parameter.Type})
}

func commandParameterTypeMatches(kind string, value any) bool {
	switch kind {
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "integer":
		number, ok := value.(float64)
		return ok && number == float64(int64(number))
	case "number":
		_, ok := value.(float64)
		return ok
	default:
		return false
	}
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// CommandDescription returns the description of a command skill.
func (s *Service) CommandDescription(name string) string {
	if skill, ok := commandSkills(s.loadSkillCatalog())[name]; ok {
		return skill.Description
	}
	return ""
}

func (s *Service) loadCommandCatalog() map[string]contract.CommandBinding {
	if s.commandCatalog == nil {
		return map[string]contract.CommandBinding{}
	}
	return s.commandCatalog()
}

func (s *Service) loadPersonaCatalog() map[string]contract.PersonaInfo {
	out := map[string]contract.PersonaInfo{}
	if s.personaCatalog == nil {
		return out
	}
	for _, p := range s.personaCatalog() {
		out[p.Slug] = p
	}
	return out
}

func (s *Service) loadSkillCatalog() map[string]contract.SkillInfo {
	out := map[string]contract.SkillInfo{}
	if s.skillCatalog == nil {
		return out
	}
	for _, sk := range s.skillCatalog() {
		out[sk.Slug] = sk
	}
	return out
}

func (s *Service) loadLawCatalog() map[string]contract.LawInfo {
	out := map[string]contract.LawInfo{}
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
func (s *Service) loadTemplateCatalogForCommand() map[string]contract.TemplateInfo {
	out := map[string]contract.TemplateInfo{}
	if s.templateCatalog == nil {
		return out
	}
	for _, t := range s.templateCatalog() {
		out[t.Slug] = contract.TemplateInfo{
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
func pickSkills(slugs []string, skills map[string]contract.SkillInfo) []contract.SkillInfo {
	if len(slugs) == 0 || len(skills) == 0 {
		return nil
	}
	out := make([]contract.SkillInfo, 0, len(slugs))
	for _, slug := range slugs {
		if sk, ok := skills[slug]; ok {
			out = append(out, sk)
		}
	}
	return out
}

func invocationArgs(args map[string]any) []contract.InvocationArg {
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
	out := make([]contract.InvocationArg, 0, len(keys))
	for _, key := range keys {
		out = append(out, contract.InvocationArg{Name: key, Value: formatInvocationArg(values[key])})
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
func effectiveLaws(globalSpec, commandSpec contract.CommandBinding, persona *contract.PersonaInfo, templates []contract.TemplateInfo, laws map[string]contract.LawInfo) []contract.LawInfo {
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

func resolveEffectiveLaws(slugs []string, disabled map[string]struct{}, laws map[string]contract.LawInfo) []contract.LawInfo {
	seen := map[string]struct{}{}
	out := []contract.LawInfo{}
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

// renderCommandMarkdown renders persona, invocation arguments, skills, laws and templates.
func renderCommandMarkdown(resp contract.ResolveCommandResponse) string {
	r := markdownRenderer{}
	r.writePersona(resp.Persona)
	r.writeInvocationArgs(resp.InvocationArgs)
	r.writeSkills(resp.Skills)
	r.writeLaws(resp.Laws)
	r.writeTemplates(resp.Templates)
	r.writeRelated(resp.Related)
	r.writeOutputLanguage(resp.AgentOutputLanguage)
	return r.b.String()
}

func (r *markdownRenderer) writeRelated(related []contract.RelatedCommand) {
	if len(related) == 0 {
		return
	}
	r.openSection("## Related commands")
	for _, command := range related {
		fmt.Fprintf(&r.b, "### %s\n", command.Name)
		if command.Description != "" {
			fmt.Fprintf(&r.b, "%s\n", command.Description)
		}
		if command.When != "" {
			fmt.Fprintf(&r.b, "Condition: %s\n", command.When)
		}
		if command.Context == "full" {
			fmt.Fprintf(&r.b, "\n%s\n", strings.TrimSpace(command.Markdown))
		}
	}
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

func (r *markdownRenderer) writePersona(persona *contract.PersonaInfo) {
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

func (r *markdownRenderer) writeInvocationArgs(args []contract.InvocationArg) {
	if len(args) == 0 {
		return
	}
	r.openSection("## Invocation Args")
	for _, arg := range args {
		fmt.Fprintf(&r.b, "- `%s`: %s\n", arg.Name, arg.Value)
	}
}

func (r *markdownRenderer) writeSkills(skills []contract.SkillInfo) {
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

func (r *markdownRenderer) writeLaws(laws []contract.LawInfo) {
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

func (r *markdownRenderer) writeTemplates(templates []contract.TemplateInfo) {
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
