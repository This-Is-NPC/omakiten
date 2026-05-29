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
//	okt → okt-project-resume / okt-task-imagine
//	  okt-task-imagine → okt-task-create
//	    okt-task-create → (move to dev) → okt-task-continue / okt-task-implement
//	      okt-project-resume → okt-task-continue
//	        okt-task-continue → okt-task-implement
//	          okt-task-implement → (move to review)
//	okt-task-document is parallel: surfaces drift; if material work is needed,
//	suggests `okt-task-create` to spin up a documentation task.
//	okt-config is parallel: orients the agent on the config layout so it can
//	answer edit questions without guessing; suggests `okt-task-implement` when
//	the user has a concrete edit in mind.
//	okt-task-commit is parallel: drafts Conventional Commits for user-authored
//	edits made outside the `okt-task-implement` loop; never auto-pushes — the
//	human owns publication.
//	okt-task-review is parallel: walks the diff through a Fowler/Beck/Martin/
//	Feathers lens; surfaces findings + refactor opportunities; read-only,
//	suggests `okt-task-implement` to apply fixes.
//	okt-task-check is parallel: discovers test/lint/audit targets, runs them
//	via Bash, emits a tabular pass/fail report; read-only, suggests
//	`okt-task-implement` for fixes or `okt-task-review` for triage.
//	okt-pause is the bare orchestrator close: synthesises a handoff note for
//	the next session. okt-note-free captures an ad-hoc note. okt-note-recap
//	renders a recap timeline and, with a wide window (e.g. `day`/cross-project),
//	folds in the former standup digest — one command spans both.
// Action texts deliberately stop short of repeating constraints already
// declared inline in `## Laws` or role-specific flow already declared in the
// persona body. Each one names the canonical tool and ends with a REST-style
// handoff. Anthropic's context-engineering guidance is the rubric: keep
// prompts at the right altitude, defer body-heavy data via just-in-time
// fetches, and let bound laws/persona body/templates do the role and
// constraint work instead of restating it in prose.
var commandActions = map[string]string{
	"okt": "Load the active project state via `project.overview`. Report the snapshot to the user. " +
		"Next: suggest `okt-project-resume` to scan likely-next work, or `okt-task-imagine` to explore a new direction.",

	"okt-task-imagine": "Open discovery — no task exists yet. Ground yourself " +
		"with `project.overview` and `tasks.list`, then interrogate the user via 5W2H (What / Why / Who / When / " +
		"Where / How / How much) — don't accept vague answers. Call `templates.show comment-5w2h` and " +
		"`templates.show comment-smart-success` to fetch the scaffolds when the user is ready to commit answers; " +
		"template-fidelity is disabled here so freewheel exploration is fine before the scaffolds land. " +
		"Frame success in SMART terms before handing off. Next: when the shape is concrete, suggest `okt-task-create`.",

	"okt-task-create": "Author the task. Apply feasibility-gate first — " +
		"infeasible requests stop here with the report, no task created. Otherwise call " +
		"`templates.show user-story` to fetch the scaffold, fill it per template-fidelity, then " +
		"`tasks.create_intent` with the filled description. The response carries `confirmation` and " +
		"`similar_tasks` when ambiguity exists — surface them to the user verbatim and let them choose. " +
		"Next: suggest the user create the branch, add a `#self-branch` comment via `comments.add` " +
		"(template_slug=`comment-selfbranch`), and move the task to dev.",

	"okt-project-resume": "Scan for next work. Call `project.resume` and report " +
		"top candidates with one-line rationale. Next: when the user picks a task, suggest `okt-task-continue` " +
		"with that task id.",

	"okt-task-continue": "Read a task's checkpoint — understand where the task stopped, " +
		"do not start coding. Call `tasks.continue` for the task id, then summarize the last decision, " +
		"open questions, and the immediate next increment. Next: suggest `okt-task-implement` with the same id.",

	"okt-task-implement": "Apply the next increment for the task. If you do not have the task state, " +
		"call `tasks.continue` first. " +
		"Next: suggest the user add a `#resume` comment via `comments.add` " +
		"(template_slug=`comment-resume`) and move the task to review.",

	"okt-task-document": "Survey `.docs/internal/architecture.md`, " +
		"`.docs/internal/requirements.md`, `README.md`, `CONTRIBUTING.md`, and other top-level docs. List drift " +
		"items with file references and suggested wording — do not edit in place. " +
		"Next: if material work is needed, suggest `okt-task-create` to spin up a documentation task.",

	"okt-config": "Orient on the active config layout. Call `templates.show config-orientation` to " +
		"load the path resolution order, entity layout, frontmatter shapes, wiring relationships, and " +
		"workflow guard kinds. Read it fully before answering any config-edit question — do not guess. " +
		"Next: if the user has a concrete edit in mind, suggest `okt-task-implement` with the change scoped to " +
		"`omakiten.yaml` or the relevant entity file.",

	"okt-task-commit": "Draft Conventional Commits for the working tree. Read `git status` and `git diff --cached` " +
		"(fall back to unstaged changes when nothing is staged). Group hunks into one intent per commit; split " +
		"mixed trees via non-interactive staging (`git add <path>` / `git restore --staged <path>`). Derive the " +
		"scope from the touched paths. Draft `<type>(<scope>): <subject>` (≤50 chars, imperative) plus an " +
		"optional 72-column body that explains the \"why\" the diff does not. Surface every draft to the user " +
		"before invoking `git commit` via Bash. Never `git push` — the human owns publication. " +
		"Next: when the working tree is clean, suggest the user `git push` when ready.",

	"okt-task-review": "Walk the diff with the loaded lens. Run `git diff <base>..HEAD` (default base `main`; use " +
		"staged when explicit) and read every hunk before writing findings. Order the pass correctness → " +
		"security → smells → refactor opportunities → scalability/performance. Cite methodology by name when " +
		"applicable (`Extract Function — Fowler`, `Feature Envy — Fowler/Beck`, `Sprout Method — Feathers`, " +
		"`OCP — Martin`). Tag every finding by severity (`error` / `warning` / `info`). Call " +
		"`templates.show comment-review-findings` and `templates.show comment-refactor-opportunities` for " +
		"the scaffolds, then post the filled comments on the task. Read-only — never edit files, never run " +
		"`git commit`. Next: when findings need fixes, suggest `okt-task-implement` with the finding ids.",

	"okt-task-check": "Run the project's check targets. Discover them via `mise tasks` first; fall back to " +
		"`npm run`, `make -qp`, `package.json > scripts`, or the repo's `CONTRIBUTING.md` — stop at the " +
		"first hit, do not guess. Invoke each target via Bash, capture stdout/stderr/exit code. Call " +
		"`templates.show comment-check-report` for the scaffold, then fill it — one row per target with " +
		"status (`pass` / `fail` / `skip` / `yellow`) and a one-line failing tail. Quote the last ≤10 " +
		"lines of stderr verbatim per failed target; never summarize errors. Read-only — never apply fixes, " +
		"never re-run after editing. Next: failures route to `okt-task-implement` with the target name + tail; " +
		"smell-level findings route to `okt-task-review` for triage.",

	"okt-pause": "Close the current session with a handoff note for the next agent. Synthesise material " +
		"state since the previous handoff via `project.overview`, `tasks.list`, and `task.activity.list` " +
		"for in-flight tasks. Call `templates.show note-handoff` to fetch the scaffold, fill the populated " +
		"slots, and persist via `notes.create` with `scope=project`, `kind=handoff`. Honor `--body` to " +
		"override the rendered body verbatim and `--note` to append extra context under a free-form " +
		"section. When nothing material changed since the last handoff, render with a \"no material " +
		"changes since <prev>\" marker and still persist so the timeline stays continuous. When the cwd " +
		"resolves no project, stop with `no project at <cwd>` and suggest `--project <slug>`; when the " +
		"project lacks an active workflow, omit the workflow/wave sections. Next: suggest the user run " +
		"`okt-note-recap` in their next session to load the handoff back into context.",

	"okt-note-free": "Capture a free-form knowledge note without ceremony. Resolve scope from `--scope` " +
		"(default `project` when the cwd resolves; explicit `--scope global` always wins). Resolve kind " +
		"from `--kind` (default `free`); reject `handoff`, `standup-digest`, and `recap` here — those " +
		"belong to their dedicated commands. Title from `--title`; body from prompt or stdin. Call " +
		"`templates.show note-free` to fetch the minimal scaffold, then persist via `notes.create`. " +
		"Reject empty body or empty title; when the cwd is ambiguous (multiple projects resolve) require " +
		"`--project <slug>`. Next: suggest `notes.list` to confirm the note landed, or `okt-note-recap` to " +
		"see it folded into the project timeline.",

	"okt-task-resume": "Cold-start a task — no prior context in this session. Call `tasks.continue` for the " +
		"task id, then reconstruct the full picture from scratch: read the description, every comment, the " +
		"dependency graph, and the latest `#resume`/`#tests-passing` checkpoints. Unlike a warm checkpoint read, " +
		"assume you know nothing — re-derive the current state, open questions, and the immediate next increment " +
		"from the artifacts. Next: suggest `okt-task-implement` with the same id once you have rebuilt context.",

	"okt-task-research": "Investigate the problem space before any solution is committed. Survey prior art with " +
		"`search` and `tasks.list`, read the relevant code and docs, and enumerate the unknowns the task must " +
		"resolve. Produce a findings digest — options, trade-offs, and open questions — read-only. Next: when the " +
		"unknowns are mapped, suggest `okt-task-validate` to pressure-test the framing.",

	"okt-task-validate": "Pressure-test the problem framing before it hardens into requirements. Challenge the " +
		"assumptions surfaced in discovery — is the problem real, is it worth solving now, what evidence backs the " +
		"demand? Surface risks and falsifiers; do not author the task here. Next: when the framing survives " +
		"scrutiny, suggest `okt-task-requirements` to capture what the solution must satisfy.",

	"okt-task-requirements": "Capture what the solution must satisfy. Elicit functional and non-functional " +
		"requirements, edge cases, and explicit acceptance criteria; separate must-have from nice-to-have. " +
		"Call `templates.show` for any bound requirements/acceptance scaffold and fill it. Read-only " +
		"with respect to the task body — record findings as the requirements baseline. Next: suggest " +
		"`okt-task-prioritize` to rank the work against alternatives.",

	"okt-task-prioritize": "Rank the work against alternatives. Score candidates with an explicit method " +
		"(MoSCoW / RICE / value-vs-effort) and record the rationale so the ordering is auditable, not arbitrary. " +
		"Call `templates.show` for the bound scoring scaffold and fill it. " +
		"Read-only. Next: when the priority is justified, suggest `okt-task-create` to author the top candidate.",

	"okt-task-decompose": "Break a coarse task into right-sized increments. Identify the seams — independently " +
		"shippable slices, each with its own acceptance criteria — and propose the subtask breakdown without " +
		"creating them blindly. Surface dependencies between slices. Next: suggest `okt-task-estimate` to size " +
		"each increment.",

	"okt-task-estimate": "Size each increment. Attach a relative estimate (points / t-shirt) to every slice with " +
		"a one-line basis-of-estimate, and flag the increments whose uncertainty dominates. Read-only. Next: when " +
		"the sizing is recorded, suggest `okt-task-design` to shape the first increment.",

	"okt-task-design": "Shape the solution before writing it. Sketch the approach — data flow, the seams you will " +
		"touch, the interfaces you will introduce — and weigh at least one alternative. Call `templates.show` for " +
		"any bound design scaffold and fill it. Record the design rationale; " +
		"do not edit production code here. Next: suggest `okt-task-implement` to build the chosen design.",

	"okt-task-self-review": "Review your OWN diff before handing it to a third party. Run `git diff <base>..HEAD` " +
		"and read every hunk you wrote with fresh eyes: dead code, leftover debug output, missing tests, unhandled " +
		"edge cases, and the gap between what you intended and what you actually changed. Call `templates.show` for " +
		"any bound findings scaffold and fill it. Distinct from the " +
		"third-party `okt-task-review` — this is the author's own pre-handoff pass. Fix trivial issues inline; " +
		"escalate the rest. Next: when your own pass is clean, suggest `okt-task-review` for a third-party lens.",

	"okt-task-refactor": "Improve the structure of code without changing its behavior. Identify a single smell " +
		"(duplication, long function, feature envy), apply the named refactoring (`Extract Function`, `Move " +
		"Method`), and keep the test suite green at every step. Call `templates.show` for any bound refactor " +
		"scaffold and fill it. One behavior-preserving transformation per pass — " +
		"no feature work. Next: suggest `okt-task-check` to confirm the suite still passes.",

	"okt-task-quality": "Assess quality through a human lens — the judgment a linter cannot make. Read the diff for " +
		"design coherence, naming, test coverage of the meaningful branches, and the smells that pass the " +
		"mechanical gate but still erode the codebase. Call `templates.show` for any bound findings scaffold and " +
		"fill it. Distinct from the pass/fail `okt-task-check` mechanical " +
		"gate — this is the qualitative read. Surface findings by severity; read-only. Next: route structural " +
		"findings to `okt-task-refactor`, behavioral gaps to `okt-task-implement`.",

	"okt-task-secure": "Walk the diff through a security lens. Trace untrusted input to sinks, check authz on every " +
		"new path, look for injection / SSRF / secret leakage / unsafe deserialization, and verify error paths do " +
		"not leak internals. Call `templates.show` for any bound findings scaffold and fill it. Cite the class of " +
		"each finding and tag it by severity; read-only — never edit. " +
		"Distinct from the general `okt-task-review`: this pass is security-only. Next: route findings to " +
		"`okt-task-implement` with the vulnerability class and location.",

	"okt-task-debrief": "Close the loop on completed work — capture what was learned, not what was done. Distill " +
		"the decisions that held, the assumptions that broke, and the follow-ups worth carrying forward, so the " +
		"next task starts smarter. Call `templates.show` for any bound lessons scaffold and fill it. Record the " +
		"debrief; read-only with respect to code. Next: suggest " +
		"`okt-task-document` if the learnings imply documentation drift.",

	"okt-note-recap": "Render a recap timeline of recent activity, scoped by the window argument. With a " +
		"single-project window (default — project from `--project <slug>` or cwd) it groups recent notes " +
		"chronologically: resolve the window from `[janela]`/`--since` (default `7d`) and the kind filter " +
		"from `--kinds <comma-list>` (default all), call `notes.list` for the project filtered by window " +
		"and kinds, then `templates.show note-recap` to fetch the scaffold; group entries by kind, order " +
		"chronologically with a timestamp prefix per bullet. With a wide window (`okt-note-recap day` or " +
		"any cross-project invocation where `--project` is omitted) it folds in the former standup digest: " +
		"enumerate every project the user owns, call `notes.list` per project filtered by `kind=handoff` " +
		"within the window (per-project limit from `--limit`, default `5`), then `templates.show " +
		"note-standup-digest` and fill one section per project ordered by most recent handoff first, " +
		"silent projects last under a clear header; when more than 50 projects resolve, paginate or " +
		"require `--project`. Read-only either way — never persist; the recap is the artifact. When zero " +
		"notes/handoffs match the window, surface \"nothing in window\" (or \"no handoffs — run okt-pause\" " +
		"for the cross-project case). Next: suggest `okt-task-continue` with a specific task id when the " +
		"recap reveals an open thread to resume.",
}

// commandDescriptions match the prompts/list metadata. Keeping them next to
// the action text means the MCP adapter can ship a single source of truth.
var commandDescriptions = map[string]string{
	"okt":                "Contextualize the agent with active Omakiten project state.",
	"okt-task-imagine":   "PLAN phase — interrogate the user via 5W2H and frame success in SMART terms before any task exists.",
	"okt-task-create":    "PLAN → DO handoff — author the task with an INVEST-checked story; record prioritization when alternatives exist.",
	"okt-project-resume": "Scan likely-next work across the active project.",
	"okt-task-continue":  "Read a task's checkpoint as an engineer before resuming work.",
	"okt-task-implement": "Execute approved engineering work with strict rigor and commit discipline.",
	"okt-task-document":  "Survey project documentation for drift and propose updates.",
	"okt-config":         "Orient the agent on the active Omakiten config layout before edits.",
	"okt-task-commit":    "Draft Conventional Commits for the working tree without pushing.",
	"okt-task-review":    "Walk the diff through Fowler/Beck/Martin/Feathers lens and surface findings + refactor opportunities.",
	"okt-task-check":     "Run discovered test/lint targets and report pass/fail in a tabular comment.",
	"okt-pause":          "Close the session with a synthesised handoff note for the next agent.",
	"okt-note-free":      "Capture a free-form knowledge note (project or global) without ceremony.",
	"okt-note-recap":     "Recap timeline of recent notes; wide window folds in the cross-project handoff digest.",
	"okt-task-resume":      "Cold-start a task from scratch — rebuild full context when none is loaded in this session.",
	"okt-task-research":    "Investigate the problem space and map the unknowns before any solution is committed.",
	"okt-task-validate":    "Pressure-test the problem framing — is it real, worth solving now, evidence-backed?",
	"okt-task-requirements": "Capture functional + non-functional requirements and explicit acceptance criteria.",
	"okt-task-prioritize":  "Rank the work against alternatives with an explicit scoring method and rationale.",
	"okt-task-decompose":   "Break a coarse task into right-sized, independently shippable increments.",
	"okt-task-estimate":    "Size each increment with a relative estimate and a one-line basis-of-estimate.",
	"okt-task-design":      "Shape the solution — approach, seams, interfaces — and weigh an alternative before coding.",
	"okt-task-self-review": "Author's own pre-handoff diff pass — distinct from the third-party review.",
	"okt-task-refactor":    "Apply one behavior-preserving structural improvement with the suite green throughout.",
	"okt-task-quality":     "Qualitative human-lens quality read — smells, coverage, design — distinct from the mechanical gate.",
	"okt-task-secure":      "Security-only diff pass — input-to-sink tracing, authz, injection, secret leakage.",
	"okt-task-debrief":     "Capture learnings from completed work — decisions that held, assumptions that broke.",
}

// CommandNames returns the canonical, ordered list of `okt-*` prompts the MCP
// adapter exposes. Order mirrors the REST-style handoff cycle so prompts/list
// answers in the order a user would naturally invoke them.
func CommandNames() []string {
	return []string{
		"okt",
		"okt-task-imagine",
		"okt-task-research",
		"okt-task-validate",
		"okt-task-requirements",
		"okt-task-prioritize",
		"okt-task-create",
		"okt-task-decompose",
		"okt-task-estimate",
		"okt-task-design",
		"okt-project-resume",
		"okt-task-resume",
		"okt-task-continue",
		"okt-task-implement",
		"okt-task-self-review",
		"okt-task-refactor",
		"okt-task-document",
		"okt-task-debrief",
		"okt-config",
		"okt-task-commit",
		"okt-task-review",
		"okt-task-secure",
		"okt-task-check",
		"okt-task-quality",
		"okt-pause",
		"okt-note-free",
		"okt-note-recap",
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
	if s.snapshot != nil {
		resp.AgentOutputLanguage = s.snapshot.AgentOutputLanguage()
	}
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
//
// The prompt name and description are NOT echoed in the body — they ship via
// `prompts/list` metadata in the MCP protocol, which every aware client
// surfaces before calling `prompts/get`. Emitting them again here would just
// duplicate bytes the agent already has.
//
// Skills render as bullet-with-body under `## Skills` — one bullet per skill
// in configured (persona-wiring) order. Each bullet carries the skill body
// (the procedural payload) when present; skills without a body fall back to
// their description, and skills with neither render as a bare name bullet.
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
			// Multi-line bodies need every continuation line indented two
			// spaces so they stay visually nested under the bullet, matching
			// the multi-line law rendering below.
			if idx := strings.Index(detail, "\n"); idx >= 0 {
				head := detail[:idx]
				tail := detail[idx+1:]
				fmt.Fprintf(&b, "- **%s** — %s\n", label, head)
				for _, line := range strings.Split(tail, "\n") {
					if line == "" {
						fmt.Fprintln(&b)
						continue
					}
					fmt.Fprintf(&b, "  %s\n", line)
				}
				continue
			}
			fmt.Fprintf(&b, "- **%s** — %s\n", label, detail)
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
			// Multi-line law bodies (those carrying Bad:/Good: examples or a
			// second paragraph) need every continuation line indented two
			// spaces so they remain visually nested under the bullet —
			// otherwise the example lines render as orphan paragraphs between
			// laws.
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

	openSection("## Action")
	fmt.Fprintf(&b, "%s\n", resp.Action)

	// Trailing output-language directive: byte-stable per session so the
	// Anthropic prompt cache hit rate stays high. Skipped entirely when
	// the user has not configured config.languages.agent_output — no
	// blank line, no marker, no observable change to existing prompts.
	if lang := strings.TrimSpace(resp.AgentOutputLanguage); lang != "" {
		fmt.Fprintf(&b, "\n**Output language:** %s\n", lang)
	}
	return b.String()
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
