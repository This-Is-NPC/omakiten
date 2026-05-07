package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"omakiten/internal/domain"
)

// commandActions stores the per-command instruction the MCP prompt lands on.
// Each action follows a REST-style hypermedia handoff: it states the role,
// the canonical tool to call, and ends by pointing at the next command in the
// flow. The cycle is:
//
//	okt → okt-resume / okt-imagine
//	  okt-imagine → okt-create
//	    okt-create → (move to dev) → okt-continue / okt-implement
//	      okt-resume → okt-continue
//	        okt-continue → okt-implement
//	          okt-implement → (move to review)
//	okt-document is parallel: surfaces drift; if material work is needed,
//	suggests `okt-create` to spin up a documentation task.
var commandActions = map[string]string{
	"okt": "Load the active Omakiten project state. Call `project.overview` to fetch identity, " +
		"pending count, workflow buckets, recent context, and the next-step prompt. " +
		"Report the snapshot to the user. " +
		"Next: suggest `okt-resume` to scan likely-next work, or `okt-imagine` if the user " +
		"wants to explore a new direction before committing to a task.",

	"okt-imagine": "Take the role of a product owner running discovery. The task does not exist yet — " +
		"sketch the problem, the actors, and possible shapes of the solution. Call `project.overview` " +
		"and `tasks.list` to ground yourself in current work; ask clarifying questions; surface " +
		"hypotheses freely (template-fidelity is intentionally disabled for this command). " +
		"Next: when the shape is clear, suggest `okt-create` to materialize the user story.",

	"okt-create": "Take the role of a product owner authoring the task. Run a feasibility check first — " +
		"if the request is not implementable in the current state, stop and report technical reasons, " +
		"blockers, and viable alternatives without creating anything. When feasible, ask any clarifying " +
		"questions still pending, then call `tasks.create_intent` with the user-story template bound to " +
		"this command (fill Description, Acceptance Criteria, Definition of Done, Scope in/out, " +
		"Feasibility note — no fabricated content). If it returns `requires_confirmation`, ask the user " +
		"whether to continue an existing task or create a separate confirmed task. " +
		"Next: suggest the user create the task branch and add a `#self-branch` comment (use the " +
		"`comment-selfbranch` template via `comments.add` with `template_slug`), then move the task " +
		"to dev to unlock `okt-continue` or `okt-implement`.",

	"okt-resume": "Take the role of an engineer scanning for next work. Call `project.resume` to identify " +
		"likely continuation points, blocked/dependent work, and recent handoff context. Report the " +
		"top candidates with one-line rationale. " +
		"Next: when the user picks a task, suggest `okt-continue` with that task id to load the checkpoint.",

	"okt-continue": "Take the role of an engineer reading a checkpoint — your only job is to understand " +
		"where the task stopped. Call `tasks.continue` for the requested task id. Only continue if the " +
		"task belongs to the active project; otherwise follow the coded guidance. Inspect the comments " +
		"and recent context, summarize the last decision, the open questions, and the immediate next " +
		"increment. Do not start coding here. " +
		"Next: suggest `okt-implement` with the same task id to execute the next increment.",

	"okt-implement": "Take the role of an engineer executing approved work. Call `tasks.continue` for the " +
		"task id (if the user did not just run `okt-continue`) so you have its current state, then " +
		"implement the next increment in small coherent steps. Add or update tests for new and impacted " +
		"behavior. Run them; on failure analyze root cause and apply targeted fixes — cap the cycle at " +
		"three attempts (bounded-self-review). When commits are needed, follow conventional-commits in " +
		"English, one intent per commit. Document material changes inline (no silent behavior changes). " +
		"Next: when the increment is validated, suggest the user add a `#resume` comment (use the " +
		"`comment-resume` template via `comments.add` with `template_slug`), then move the task to " +
		"review to surface it for closing review.",

	"okt-document": "Take the role of a documentation curator. Survey the project's narrative artifacts — " +
		"`.docs/architecture.md`, `.docs/requirements.md`, `README.md`, `CONTRIBUTING.md`, and any other " +
		"top-level docs. Compare each claim against current code: dependency versions, file paths, " +
		"declared patterns, public surface. List drift items with file references and suggested wording. " +
		"Do not edit in place. " +
		"Next: if material work is needed, suggest `okt-create` to spin up a documentation task that the " +
		"engineer persona will execute via the regular workflow.",
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
			fmt.Fprintf(&b, "- **[%s] %s** — %s\n", law.Severity, label, strings.TrimSpace(law.Body))
		}
	}

	if len(resp.Templates) > 0 {
		fmt.Fprintf(&b, "\n## Templates\n")
		for _, t := range resp.Templates {
			heading := t.Name
			if heading == "" {
				heading = t.Slug
			}
			if t.Default != "" {
				fmt.Fprintf(&b, "\n### %s (default: %s)\n", heading, t.Default)
			} else {
				fmt.Fprintf(&b, "\n### %s\n", heading)
			}
			fmt.Fprintf(&b, "%s\n", strings.TrimSpace(t.Body))
		}
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
