package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"omakiten/internal/domain"
)

// commandActions stores the per-command instruction the MCP prompt lands on.
// Each action follows a REST-style hypermedia handoff: it names the canonical
// tool to call and points at the next command in the flow. The cycle is:
//
//	okt → okt-resume / okt-imagine
//	  okt-imagine → okt-create
//	    okt-create → (move to dev) → okt-continue / okt-implement
//	      okt-resume → okt-continue
//	        okt-continue → okt-implement
//	          okt-implement → (move to review)
//	okt-document is parallel: surfaces drift; if material work is needed,
//	suggests `okt-create` to spin up a documentation task.
// Action texts deliberately stop short of repeating constraints already
// declared inline in `## Laws` or role-specific flow already declared in the
// persona body. Each one names the canonical tool and ends with a REST-style
// handoff. Anthropic's context-engineering guidance is the rubric: keep
// prompts at the right altitude, defer body-heavy data via just-in-time
// fetches, and let bound laws/persona body/templates do the role and
// constraint work instead of restating it in prose.
var commandActions = map[string]string{
	"okt": "Load the active project state via `project.overview`. Report the snapshot to the user. " +
		"Next: suggest `okt-resume` to scan likely-next work, or `okt-imagine` to explore a new direction.",

	"okt-imagine": "Open discovery — no task exists yet. Ground yourself " +
		"with `project.overview` and `tasks.list`, ask clarifying questions, and sketch hypotheses freely " +
		"(template-fidelity is disabled here on purpose). Next: when the shape is clear, suggest `okt-create`.",

	"okt-create": "Author the task. Apply feasibility-gate first — " +
		"infeasible requests stop here with the report, no task created. Otherwise call " +
		"`templates.show user-story` to fetch the scaffold, fill it per template-fidelity, then " +
		"`tasks.create_intent` with the filled description. The response carries `confirmation` and " +
		"`similar_tasks` when ambiguity exists — surface them to the user verbatim and let them choose. " +
		"Next: suggest the user create the branch, add a `#self-branch` comment via `comments.add` " +
		"(template_slug=`comment-selfbranch`), and move the task to dev.",

	"okt-resume": "Scan for next work. Call `project.resume` and report " +
		"top candidates with one-line rationale. Next: when the user picks a task, suggest `okt-continue` " +
		"with that task id.",

	"okt-continue": "Read a task's checkpoint — understand where the task stopped, " +
		"do not start coding. Call `tasks.continue` for the task id, then summarize the last decision, " +
		"open questions, and the immediate next increment. Next: suggest `okt-implement` with the same id.",

	"okt-implement": "Apply the next increment for the task. If you do not have the task state, " +
		"call `tasks.continue` first. " +
		"Next: suggest the user add a `#resume` comment via `comments.add` " +
		"(template_slug=`comment-resume`) and move the task to review.",

	"okt-document": "Survey `.docs/architecture.md`, " +
		"`.docs/requirements.md`, `README.md`, `CONTRIBUTING.md`, and other top-level docs. List drift " +
		"items with file references and suggested wording — do not edit in place. " +
		"Next: if material work is needed, suggest `okt-create` to spin up a documentation task.",
}

// commandDescriptions match the prompts/list metadata. Keeping them next to
// the action text means the MCP adapter can ship a single source of truth.
var commandDescriptions = map[string]string{
	"okt":           "Contextualize the agent with active Omakiten project state.",
	"okt-imagine":   "Brainstorm freely as a product owner before any task exists.",
	"okt-create":    "Author a task as a product owner: feasibility, user story, and scope.",
	"okt-resume":    "Scan likely-next work across the active project.",
	"okt-continue":  "Read a task's checkpoint as an engineer before resuming work.",
	"okt-implement": "Execute approved engineering work with strict rigor and commit discipline.",
	"okt-document":  "Survey project documentation for drift and propose updates.",
}

// CommandNames returns the canonical, ordered list of `okt-*` prompts the MCP
// adapter exposes. Order mirrors the REST-style handoff cycle so prompts/list
// answers in the order a user would naturally invoke them.
func CommandNames() []string {
	return []string{
		"okt",
		"okt-imagine",
		"okt-create",
		"okt-resume",
		"okt-continue",
		"okt-implement",
		"okt-document",
	}
}

// CommandDescription returns the prompts/list description for a known command,
// or the empty string when the command name is unknown.
func CommandDescription(name string) string {
	return commandDescriptions[strings.TrimSpace(name)]
}

// CommandActionFallback returns the bare action text for `name` without any
// persona/laws/templates wiring. The MCP adapter falls back to this when no
// service is wired (test harnesses, partially initialized runtimes), so a
// prompts/get call still produces a usable message even before bindings are
// available.
func CommandActionFallback(name string) string {
	return commandActions[strings.TrimSpace(name)]
}

// ResolveCommand assembles the persona/skills/laws/templates package bound to
// `name` and returns it both structured and rendered as a single markdown
// message ready for an MCP PromptMessage. The resolution follows the rules
// documented in `.docs/guards-guide.md`:
//
//   - effective laws = global ∪ persona.laws ∪ command.laws ∪ templates[].laws,
//     minus command.laws_disabled, deduped, in first-seen order;
//   - persona is the one declared on the command spec (no default);
//   - skills come from the persona's wiring;
//   - templates are the slugs declared on the command spec.
//
// Missing catalogs degrade gracefully — an unwired runtime still returns the
// command's action text so the MCP harness keeps working through the upgrade.
func (s *Service) ResolveCommand(_ context.Context, input ResolveCommandInput) (ResolveCommandResponse, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return ResolveCommandResponse{}, domain.NewError(domain.ErrValidation, "command name is required", nil)
	}
	action, known := commandActions[name]
	if !known {
		return ResolveCommandResponse{}, domain.NewError(domain.ErrValidation, "unknown MCP command", map[string]any{"name": name})
	}

	resp := ResolveCommandResponse{
		Name:        name,
		Description: commandDescriptions[name],
		Action:      action,
	}

	commands := s.loadCommandCatalog()
	personas := s.loadPersonaCatalog()
	skills := s.loadSkillCatalog()
	laws := s.loadLawCatalog()
	templates := s.loadTemplateCatalogForCommand()

	spec := commands[name]
	globalSpec := commands[MCPCommandsGlobalKey]

	if spec.Persona != "" {
		if persona, ok := personas[spec.Persona]; ok {
			info := persona
			resp.Persona = &info
			resp.Skills = pickSkillsForPersona(persona, skills)
		}
	}

	for _, slug := range spec.Templates {
		if t, ok := templates[slug]; ok {
			resp.Templates = append(resp.Templates, t)
		}
	}

	resp.Laws = effectiveLaws(globalSpec, spec, resp.Persona, resp.Templates, laws)
	resp.Markdown = renderCommandMarkdown(resp)
	return resp, nil
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

func pickSkillsForPersona(persona PersonaInfo, skills map[string]SkillInfo) []SkillInfo {
	if len(persona.Skills) == 0 || len(skills) == 0 {
		return nil
	}
	out := make([]SkillInfo, 0, len(persona.Skills))
	for _, slug := range persona.Skills {
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
// templates → action) so the agent can scan them top-down without reordering.
func renderCommandMarkdown(resp ResolveCommandResponse) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", resp.Name)
	if resp.Description != "" {
		fmt.Fprintf(&b, "\n%s\n", resp.Description)
	}

	if resp.Persona != nil {
		fmt.Fprintf(&b, "\n## Persona — %s\n", resp.Persona.Name)
		if resp.Persona.Description != "" {
			fmt.Fprintf(&b, "%s\n", resp.Persona.Description)
		}
		if body := strings.TrimSpace(resp.Persona.Body); body != "" {
			fmt.Fprintf(&b, "\n%s\n", body)
		}
	}

	if len(resp.Skills) > 0 {
		names := make([]string, 0, len(resp.Skills))
		for _, sk := range resp.Skills {
			names = append(names, sk.Name)
		}
		fmt.Fprintf(&b, "\n## Skills — %s\n", strings.Join(names, ", "))
		for _, sk := range resp.Skills {
			if desc := strings.TrimSpace(sk.Description); desc != "" {
				fmt.Fprintf(&b, "- **%s**: %s\n", sk.Name, desc)
			} else {
				fmt.Fprintf(&b, "- **%s**\n", sk.Name)
			}
		}
	}

	if len(resp.Laws) > 0 {
		fmt.Fprintf(&b, "\n## Laws (%d)\n", len(resp.Laws))
		for _, law := range resp.Laws {
			label := law.Name
			if label == "" {
				label = law.Slug
			}
			body := strings.TrimSpace(law.Body)
			// Multi-line law bodies (those carrying ❌/✅ examples or a second
			// paragraph) need every continuation line indented two spaces so
			// they remain visually nested under the bullet — otherwise the
			// example lines render as orphan paragraphs between laws.
			if idx := strings.Index(body, "\n"); idx >= 0 {
				head := body[:idx]
				tail := body[idx+1:]
				fmt.Fprintf(&b, "- **[%s] %s** — %s\n", law.Severity, label, head)
				for _, line := range strings.Split(tail, "\n") {
					if line == "" {
						fmt.Fprintln(&b)
						continue
					}
					fmt.Fprintf(&b, "  %s\n", line)
				}
				continue
			}
			fmt.Fprintf(&b, "- **[%s] %s** — %s\n", law.Severity, label, body)
		}
	}

	if len(resp.Templates) > 0 {
		// Just-in-time pattern: ship template metadata only, never the body.
		// Bodies are large (the pull-request scaffold alone is ~700 tokens) and
		// the agent only needs them at the moment of materialization. The
		// per-command action text instructs the agent to call
		// `templates.show <slug>` when it is ready to fill the scaffold; the
		// `template-fidelity` law ships inline as a constraint reminder so the
		// fetch happens with the right framing. This trades one extra MCP
		// round-trip on the rare materialization step for hundreds of tokens
		// saved on every prompt resolution.
		fmt.Fprintf(&b, "\n## Templates\n")
		for _, t := range resp.Templates {
			label := t.Name
			if label == "" {
				label = t.Slug
			}
			line := fmt.Sprintf("- **%s**", t.Slug)
			if label != t.Slug {
				line += fmt.Sprintf(" — %s", label)
			}
			if t.Default != "" {
				line += fmt.Sprintf(" (default: %s)", t.Default)
			}
			if desc := strings.TrimSpace(t.Description); desc != "" {
				line += fmt.Sprintf(" — %s", desc)
			}
			fmt.Fprintln(&b, line)
		}
		fmt.Fprintln(&b, "\nFetch the body with `templates.show <slug>` when ready to fill it.")
	}

	fmt.Fprintf(&b, "\n## Action\n%s\n", resp.Action)
	return b.String()
}

// SortedCommandNames is exposed for tests that want a stable iteration order.
// It is a thin wrapper around CommandNames; both are in the agent layer so
// the MCP adapter does not have to maintain its own list.
func SortedCommandNames() []string {
	out := append([]string(nil), CommandNames()...)
	sort.Strings(out)
	return out
}
