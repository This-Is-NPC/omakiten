package agent

import "strings"

// oktStartAction is the Concierge entry playbook shared by both the bare `okt`
// shortcut and the explicit `okt-start`. It is the smart entry that replaces
// the old thin `okt` router: it reads the latest handoff/recap notes and the
// plan/board state, then PROPOSES concrete next commands AND teaches the
// options available — guiding, not execute-only. The load-bearing phrases
// (reads notes handoff/recap, reads plan + board state, proposes concrete next
// commands, suggest a plan when the board has tasks but no plan, teaches the
// available options) are pinned by the okt-start smoke test in agentruntime.
//
// Both the `okt` and `okt-start` rows of commandTable point at this one const,
// so the bare shortcut and the explicit command resolve to byte-identical
// action text without a string-alias table — the okt→okt-start smoke test
// pins the sharing.
const oktStartAction = "Open the session as the concierge: orient the user, then hand them the next move. Read the " +
	"active picture first — `project.overview` for the board snapshot, `tasks.list` for in-flight work, " +
	"`plans.list` for the plan state, and `comments.list` with `scope=project` (kind `handoff` and `recap`, most " +
	"recent first) to recover " +
	"the latest HANDOFF/RECAP so you resume the thread the previous session left, not a cold start. " +
	"PROPOSE CONCRETE NEXT COMMANDS from what you read — name the actual command, not a vague direction: when a " +
	"handoff points at an open task, suggest `okt-task-continue` with that id; when a plan has a claimable task, " +
	"suggest `okt-plan-continue` / `okt-plan-claim`; when the board is empty or the user has a fresh idea, suggest " +
	"`okt-shape` to shape it into ready tasks; when work is ready to drive, suggest `okt-run`. " +
	"SUGGEST CREATING A PLAN when the board has tasks but no plan groups them — loose tasks with no plan are a gap, " +
	"so point the user at `okt-shape` (or `okt-plan-create` directly) to organize them into waves before driving. " +
	"TEACH THE AVAILABLE OPTIONS as you go: briefly say what each suggested command does and when to reach for it, " +
	"so the user is choosing among understood moves rather than guessing — the entry coaches, it does not just " +
	"route. When the cwd resolves no project, stop with `no project at <cwd>` and suggest `--project <slug>`. " +
	"Next: surface the single best next command for the current state, with the runner-up alternatives named so " +
	"the user can override your pick."

// commandEntry is one row of the single source of truth for the `okt-*` MCP
// prompt surface. Slug is the prompt name, Action is the instruction text the
// resolver lands on, and Description is the prompts/list metadata.
type commandEntry struct {
	Slug        string
	Action      string
	Description string
}

// commandTable is the ONE ordered source of truth for the command surface.
// Its order IS the canonical prompts/list order (the REST-style handoff cycle),
// so CommandNames() simply projects the Slug column. CommandActionFallback and
// CommandDescription index into it by slug. Replacing the former three parallel
// maps/slices (commandActions, commandDescriptions, CommandNames) keeps the
// three facets impossible to drift apart.
//
// Action texts follow a REST-style hypermedia handoff: each names the canonical
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
//
// Action texts deliberately stop short of repeating constraints already
// declared inline in `## Laws` or role-specific flow already declared in the
// persona body. Each one names the canonical tool and ends with a REST-style
// handoff. Anthropic's context-engineering guidance is the rubric: keep
// prompts at the right altitude, defer body-heavy data via just-in-time
// fetches, and let bound laws/persona body/templates do the role and
// constraint work instead of restating it in prose.
//
// The bare `okt` and the explicit `okt-start` share oktStartAction so a user
// who types `okt` gets the smart entry, not a thin router.
var commandTable = []commandEntry{
	{
		Slug:        "okt",
		Action:      oktStartAction,
		Description: "Smart entry — shortcut to okt-start: reads handoffs/recaps + plan/board and proposes the next command.",
	},
	{
		Slug: "okt-help",
		// okt-help is the system-tier orientation command: it teaches HOW omakiten
		// works rather than executing project work. It is tier-aware — it names the
		// three command tiers (orchestrators / system / granular), walks the
		// start → shape → run → audit → pause mental flow, and gives decision hints
		// for WHEN to drop from an orchestrator to the granular okt-task-* /
		// okt-plan-* surface. The load-bearing phrases (the three tier names, the
		// mental-flow command chain, the drop-to-granular hint) are pinned by the
		// CW7 okt-help smoke test in agentruntime.
		Action: "Orient the user on how omakiten works and help them organize their work — this is the tutorial, " +
			"not a project action. Teach the surface in three layers.\n\n" +
			"THE COMMAND TIERS — omakiten's `okt-*` commands fall into three tiers, and naming the tier tells the user " +
			"which altitude they are operating at:\n" +
			"- ORCHESTRATORS (bare, primary path): `okt-start`, `okt-shape`, `okt-run`, `okt-audit`, `okt-pause` (and " +
			"the bare `okt`, a shortcut to `okt-start`). These are the director commands — they read state, propose the " +
			"next move, and delegate the surgical work. Reach for these first; they are where most sessions live.\n" +
			"- SYSTEM (bare, talk to the TOOL not the project): `okt-help` (this), `okt-config` (orient on / customize " +
			"the config + environment), `okt-skill <slug>` (load a skill body, or list the catalog). No project object — " +
			"they configure or explain omakiten itself.\n" +
			"- GRANULAR (object-namespaced `okt-<object>-<verb>`): the power-user, surgical surface — `okt-task-*`, " +
			"`okt-plan-*`, `okt-project-*`, `okt-note-*`. One precise step each (implement, review, secure, claim, …).\n\n" +
			"THE MENTAL FLOW — a session normally walks `okt-start` → `okt-shape` → `okt-run` → `okt-audit` → `okt-pause`: " +
			"START to orient and pick up the prior thread, SHAPE a raw idea or loose backlog into ready tasks plus a plan, " +
			"RUN to drive that plan to completion by delegation, AUDIT for a deep assurance pass, then PAUSE to snapshot a " +
			"handoff note for the next session. The orchestrators each name the next command, so the flow self-advances.\n\n" +
			"WHEN TO DROP TO GRANULAR — stay on the orchestrators for the normal path; drop to the granular okt-task-* / " +
			"okt-plan-* commands when you need a single surgical step the orchestrator would otherwise delegate: building " +
			"one task by hand (`okt-task-continue` → `okt-task-implement`), running just a review (`okt-task-review`), a " +
			"security-only pass (`okt-task-secure`), or claiming one plan task (`okt-plan-claim`). Rule of thumb: " +
			"orchestrators decide and delegate; granulars do the one thing — reach for a granular when you already know " +
			"the exact step and want to skip the director. " +
			"Next: suggest `okt-start` to begin a session, `okt-config` to customize the environment, or " +
			"`okt-skill` to browse the available skills.",
		Description: "System command — tier-aware guide to how omakiten works: the orchestrator/system/granular tiers, the start→shape→run→audit→pause flow, and when to drop to granular commands.",
	},
	{
		Slug:        "okt-start",
		Action:      oktStartAction,
		Description: "Concierge entry — reads handoffs/recaps + plan/board state, proposes concrete next commands, and teaches the options.",
	},
	{
		Slug: "okt-shape",
		// okt-shape is the Owner shaping orchestrator: it carries a raw idea or a
		// loose backlog from inception to ready-to-build — discover → define →
		// plan — and GUIDES at every fork, naming the granular command for each
		// step and teaching when each is worth running rather than executing them
		// blindly. The load-bearing phrases below (chain the discover/define
		// granulars, then okt-plan-create; surface what is still undefined) are
		// pinned by the okt-shape smoke test in agentruntime.
		Action: "Shape a raw idea — or a loose backlog — into ready-to-build tasks plus an execution plan. You " +
			"orchestrate the shaping; you do not implement. Read the current picture first with `project.overview` and " +
			"`tasks.list` so you shape against what already exists, not in a vacuum. " +
			"CHAIN THE DISCOVER → DEFINE GRANULARS, directing by command NAME only — do not render their bodies. " +
			"DISCOVER the problem space: `okt-task-research` to map prior art and unknowns, then `okt-task-validate` to " +
			"pressure-test whether the problem is real and worth solving now. DEFINE the solution: `okt-task-requirements` " +
			"to capture functional/non-functional criteria, `okt-task-prioritize` to rank against alternatives, then " +
			"`okt-task-create` to author each ready task with an INVEST-checked story. For coarse work, slot " +
			"`okt-task-decompose` and `okt-task-estimate` between define and create to right-size the slices. " +
			"COACH THE DECISION at each fork: skip discovery only when the problem is already well-understood; do not " +
			"author a task whose value or feasibility is still unproven — loop back to validate instead. A shaping pass " +
			"is done when each candidate is a concrete, prioritized, ready task. " +
			"GROUP THE READY TASKS INTO A PLAN with `okt-plan-create`: settle the slug, name, and goal_body, then lay the " +
			"tasks into ordered waves so dependencies fall across wave boundaries. Suggest a plan whenever the shaping " +
			"produced more than one ready task or any dependency between them. " +
			"SURFACE WHAT IS STILL UNDEFINED before you hand off: list every gap — unanswered requirement, unranked " +
			"candidate, unestimated coarse task, missing acceptance criterion, unresolved dependency — so the user sees " +
			"exactly what blocks build, rather than discovering it mid-implementation. " +
			"Next: once the plan is assembled and the gaps are named, suggest `okt-run` to drive the plan to completion, " +
			"or `okt-task-continue` with a specific id when the user wants to build one task by hand.",
		Description: "Owner orchestrator — shape a raw idea or backlog into ready tasks + an execution plan; chains discover/define + okt-plan-create and surfaces gaps.",
	},
	{
		Slug: "okt-run",
		// okt-run is the Owner director playbook: a LEAN orchestrator that drives a
		// plan (or a single task) to completion by spawning ONE Builder subagent per
		// task via the Agent tool, then reviewing each compact return. It NEVER
		// renders or holds `okt-task-*` bodies — the delegation contract instructs
		// each Builder to invoke those granular commands ITSELF through its own MCP
		// access, in the Builder's own fresh context. The engine is subagents, not a
		// workflow. The load-bearing contract phrases below (spawn-per-task,
		// lean-context / subagent-invokes-its-own-commands, conditional-parallel
		// gating, compact structured return, clean halt on failure) are pinned by
		// the okt-run smoke test in agentruntime.
		Action: "Direct a plan — or a single task — to completion by delegation. You are the Owner: you " +
			"orchestrate, you do not implement. Detect the target from context: a task id runs that one task, a plan " +
			"id (or slug) runs that plan, and a bare invocation resolves the current plan via `plans.continue` / " +
			"`project.overview`. " +
			"For a plan, read its scope with `plans.show` / `plans.continue` and read each candidate task's " +
			"dependency graph with `dependencies.list` — do NOT load the `okt-task-*` command bodies; you direct by " +
			"command NAME only, keeping this context lean. " +
			"SELECT runnable tasks: a task is runnable only when every dependency it has is already satisfied. Tasks " +
			"with unmet dependencies WAIT. " +
			"SPAWN ONE BUILDER SUBAGENT PER TASK via the Agent tool. The delegation contract you hand each Builder is " +
			"lean — it names the task id and instructs the Builder to INVOKE THE GRANULAR `okt-task-*` COMMANDS ITSELF " +
			"via its OWN MCP access in its OWN FRESH CONTEXT (typically `okt-task-resume` or `okt-task-continue`, then " +
			"`okt-task-implement` / `okt-task-self-review` / `okt-task-refactor` / `okt-task-check`). You NEVER render, " +
			"hold, or pass the body of any `okt-task-*` command — the Builder fetches each one itself. " +
			"CONDITIONAL PARALLELISM: run independent tasks concurrently ONLY when their dependencies are satisfied AND " +
			"concurrency is worthwhile (disjoint surfaces, no shared files, enough work to justify the coordination) — " +
			"never parallelize everything; when in doubt, run sequentially. " +
			"COMPACT RETURN: instruct each Builder to return a compact, structured result — a diff summary plus " +
			"`#tests-passing` evidence (the check tail / passing count) — NOT its full working context. You review " +
			"that return only: accept it, reject it with a reason, or re-spawn a fresh Builder for the same task. This " +
			"is a lightweight director acceptance gate, not a third-party code review — deep review lives in " +
			"`okt-audit`; do not duplicate it here. " +
			"HALT CLEANLY on the first task whose Builder returns failing or blocked: stop spawning, report the final " +
			"state (which tasks accepted, which one halted and why, which remain), and leave the run resumable so the " +
			"user can re-invoke `okt-run` from the halted task. " +
			"Next: when every selected task is accepted, suggest `okt-audit` for a deep review pass, or `okt-pause` to " +
			"synthesise a handoff note.",
		Description: "Owner orchestrator — drive a plan or task to completion by spawning a Builder subagent per task and reviewing each compact return; conditional parallelism.",
	},
	{
		Slug: "okt-task-imagine",
		Action: "Open discovery — no task exists yet. Ground yourself " +
			"with `project.overview` and `tasks.list`, then interrogate the user via 5W2H (What / Why / Who / When / " +
			"Where / How / How much) — don't accept vague answers. Call `templates.show comment-5w2h` and " +
			"`templates.show comment-smart-success` to fetch the scaffolds when the user is ready to commit answers; " +
			"template-fidelity is disabled here so freewheel exploration is fine before the scaffolds land. " +
			"Frame success in SMART terms before handing off. Next: when the shape is concrete, suggest `okt-task-create`.",
		Description: "PLAN phase — interrogate the user via 5W2H and frame success in SMART terms before any task exists.",
	},
	{
		Slug: "okt-task-research",
		Action: "Investigate the problem space before any solution is committed. Survey prior art with " +
			"`search` and `tasks.list`, read the relevant code and docs, and enumerate the unknowns the task must " +
			"resolve. Produce a findings digest — options, trade-offs, and open questions — read-only. Next: when the " +
			"unknowns are mapped, suggest `okt-task-validate` to pressure-test the framing.",
		Description: "Investigate the problem space and map the unknowns before any solution is committed.",
	},
	{
		Slug: "okt-task-validate",
		Action: "Pressure-test the problem framing before it hardens into requirements. Challenge the " +
			"assumptions surfaced in discovery — is the problem real, is it worth solving now, what evidence backs the " +
			"demand? Surface risks and falsifiers; do not author the task here. Next: when the framing survives " +
			"scrutiny, suggest `okt-task-requirements` to capture what the solution must satisfy.",
		Description: "Pressure-test the problem framing — is it real, worth solving now, evidence-backed?",
	},
	{
		Slug: "okt-task-requirements",
		Action: "Capture what the solution must satisfy. Elicit functional and non-functional " +
			"requirements, edge cases, and explicit acceptance criteria; separate must-have from nice-to-have. " +
			"Call `templates.show` for any bound requirements/acceptance scaffold and fill it. Read-only " +
			"with respect to the task body — record findings as the requirements baseline. Next: suggest " +
			"`okt-task-prioritize` to rank the work against alternatives.",
		Description: "Capture functional + non-functional requirements and explicit acceptance criteria.",
	},
	{
		Slug: "okt-task-prioritize",
		Action: "Rank the work against alternatives. Score candidates with an explicit method " +
			"(MoSCoW / RICE / value-vs-effort) and record the rationale so the ordering is auditable, not arbitrary. " +
			"Call `templates.show` for the bound scoring scaffold and fill it. " +
			"Read-only. Next: when the priority is justified, suggest `okt-task-create` to author the top candidate.",
		Description: "Rank the work against alternatives with an explicit scoring method and rationale.",
	},
	{
		Slug: "okt-task-create",
		Action: "Author the task. Apply feasibility-gate first — " +
			"infeasible requests stop here with the report, no task created. Otherwise call " +
			"`templates.show user-story` to fetch the scaffold, fill it per template-fidelity, then " +
			"`tasks.create_intent` with the filled description. The response carries `confirmation` and " +
			"`similar_tasks` when ambiguity exists — surface them to the user verbatim and let them choose. " +
			"Next: suggest the user create the branch, add a `#self-branch` comment via `comments.add` " +
			"(template_slug=`comment-selfbranch`), and move the task to dev.",
		Description: "PLAN → DO handoff — author the task with an INVEST-checked story; record prioritization when alternatives exist.",
	},
	{
		Slug: "okt-task-decompose",
		Action: "Break a coarse task into right-sized increments. Identify the seams — independently " +
			"shippable slices, each with its own acceptance criteria — and propose the subtask breakdown without " +
			"creating them blindly. Surface dependencies between slices. Next: suggest `okt-task-estimate` to size " +
			"each increment.",
		Description: "Break a coarse task into right-sized, independently shippable increments.",
	},
	{
		Slug: "okt-task-estimate",
		Action: "Size each increment. Attach a relative estimate (points / t-shirt) to every slice with " +
			"a one-line basis-of-estimate, and flag the increments whose uncertainty dominates. Read-only. Next: when " +
			"the sizing is recorded, suggest `okt-task-design` to shape the first increment.",
		Description: "Size each increment with a relative estimate and a one-line basis-of-estimate.",
	},
	{
		Slug: "okt-task-design",
		Action: "Shape the solution before writing it. Sketch the approach — data flow, the seams you will " +
			"touch, the interfaces you will introduce — and weigh at least one alternative. Call `templates.show` for " +
			"any bound design scaffold and fill it. Record the design rationale; " +
			"do not edit production code here. Next: suggest `okt-task-implement` to build the chosen design.",
		Description: "Shape the solution — approach, seams, interfaces — and weigh an alternative before coding.",
	},
	{
		Slug: "okt-project-resume",
		Action: "Scan for next work. Call `project.resume` and report " +
			"top candidates with one-line rationale. Next: when the user picks a task, suggest `okt-task-continue` " +
			"with that task id.",
		Description: "Scan likely-next work across the active project.",
	},
	{
		Slug: "okt-project-continue",
		Action: "Warm-resume the current project from the last session. Assume continuity — you have " +
			"recent context. Call `project.overview` for the active snapshot and `tasks.list` for in-flight work, then " +
			"pick up the most recent open thread without re-deriving the whole project from scratch. Unlike the cold " +
			"`okt-project-resume` scan, this is the warm hand-back: surface what changed since last session and the " +
			"immediate next move. Next: suggest `okt-task-continue` with the in-flight task id to read its checkpoint.",
		Description: "Warm-resume the project from the last session — pick up the open thread.",
	},
	{
		Slug: "okt-plan-create",
		Action: "Author a WBS-style plan that groups child tasks into ordered waves. Settle the slug " +
			"(kebab-case, unique per project), a human-readable name, and a markdown `goal_body` stating the plan's " +
			"intent and acceptance criteria before committing. Call `plans.create` with the filled fields. " +
			"Next: suggest `plans.add_wave` to lay out the waves, then `okt-plan-show` to inspect the assembled plan.",
		Description: "Author a WBS-style plan grouping child tasks into ordered waves with a goal body.",
	},
	{
		Slug: "okt-plan-show",
		Action: "Inspect one plan's structure. Call `plans.show` for the slug and report the wave layout, " +
			"per-wave and overall done/total counts, the integer percent complete, and the active wave (lowest-position " +
			"wave with pending work). Read-only — surface the snapshot, do not mutate. Next: suggest `okt-plan-continue` " +
			"to preview the next claimable task, or `okt-plan-claim` when the user is ready to reserve it.",
		Description: "Inspect one plan — wave layout, done/total counts, percent, and the active wave.",
	},
	{
		Slug: "okt-plan-continue",
		Action: "Preview a plan before committing to a claim. Call `plans.continue` for the slug: it returns " +
			"the full plan aggregate (waves, done/total, active wave) plus a non-mutating preview of the task " +
			"`plans.claim_next` would reserve next. Inspect the goal_body, wave layout, and candidate task, then report " +
			"them. Read-only — nothing is reserved here. Next: suggest `okt-plan-claim` to atomically reserve the " +
			"previewed task.",
		Description: "Preview a plan plus the next claimable task before committing to a claim.",
	},
	{
		Slug: "okt-plan-claim",
		Action: "Reserve the next claimable task in the plan's active wave. Call `plans.claim_next` for the " +
			"slug — it atomically stamps the task with the caller and emits `task.assigned`, but does not move the " +
			"bucket. Report the claimed task id, or surface `claimed=false` when no unassigned first-bucket task remains " +
			"in the active wave. Next: suggest `tasks.move` to advance the claimed task once the preset guards are " +
			"satisfied, then `okt-task-continue` with the claimed id to start work.",
		Description: "Atomically reserve the next claimable task in the plan's active wave.",
	},
	{
		Slug: "okt-task-resume",
		Action: "Cold-start a task — no prior context in this session. Call `tasks.continue` for the " +
			"task id, then reconstruct the full picture from scratch: read the description, every comment, the " +
			"dependency graph, and the latest `#resume`/`#tests-passing` checkpoints. Unlike a warm checkpoint read, " +
			"assume you know nothing — re-derive the current state, open questions, and the immediate next increment " +
			"from the artifacts. Next: suggest `okt-task-implement` with the same id once you have rebuilt context.",
		Description: "Cold-start a task from scratch — rebuild full context when none is loaded in this session.",
	},
	{
		Slug: "okt-task-continue",
		Action: "Read a task's checkpoint — understand where the task stopped, " +
			"do not start coding. Call `tasks.continue` for the task id, then summarize the last decision, " +
			"open questions, and the immediate next increment. Next: suggest `okt-task-implement` with the same id.",
		Description: "Read a task's checkpoint before resuming work.",
	},
	{
		Slug: "okt-task-implement",
		Action: "Apply the next increment for the task. If you do not have the task state, " +
			"call `tasks.continue` first. When opening a PR or recording test evidence, call `templates.show` " +
			"for the bound scaffold (e.g. `templates.show pull-request`, `templates.show comment-tests-passing`) " +
			"and fill it per template-fidelity. " +
			"Next: suggest the user add a `#resume` comment via `comments.add` " +
			"(template_slug=`comment-resume`) and move the task to review.",
		Description: "Execute approved implementation work with strict rigor and commit discipline.",
	},
	{
		Slug: "okt-task-self-review",
		Action: "Review your OWN diff before handing it to a third party. Run `git diff <base>..HEAD` " +
			"and read every hunk you wrote with fresh eyes: dead code, leftover debug output, missing tests, unhandled " +
			"edge cases, and the gap between what you intended and what you actually changed. Call `templates.show` for " +
			"any bound findings scaffold and fill it. Distinct from the " +
			"third-party `okt-task-review` — this is the author's own pre-handoff pass. Fix trivial issues inline; " +
			"escalate the rest. Next: when your own pass is clean, suggest `okt-task-review` for a third-party lens.",
		Description: "Author's own pre-handoff diff pass — distinct from the third-party review.",
	},
	{
		Slug: "okt-task-refactor",
		Action: "Improve the structure of code without changing its behavior. Identify a single smell " +
			"(duplication, long function, feature envy), apply the named refactoring (`Extract Function`, `Move " +
			"Method`), and keep the test suite green at every step. Call `templates.show` for any bound refactor " +
			"scaffold and fill it. One behavior-preserving transformation per pass — " +
			"no feature work. Next: suggest `okt-task-check` to confirm the suite still passes.",
		Description: "Apply one behavior-preserving structural improvement with the suite green throughout.",
	},
	{
		Slug: "okt-task-document",
		Action: "Survey `.docs/internal/architecture.md`, " +
			"`.docs/internal/requirements.md`, `README.md`, `CONTRIBUTING.md`, and other top-level docs. List drift " +
			"items with file references and suggested wording — do not edit in place. " +
			"Next: if material work is needed, suggest `okt-task-create` to spin up a documentation task.",
		Description: "Survey project documentation for drift and propose updates.",
	},
	{
		Slug: "okt-task-debrief",
		Action: "Close the loop on completed work — capture what was learned, not what was done. Distill " +
			"the decisions that held, the assumptions that broke, and the follow-ups worth carrying forward, so the " +
			"next task starts smarter. Call `templates.show` for any bound lessons scaffold and fill it. Record the " +
			"debrief; read-only with respect to code. Next: suggest " +
			"`okt-task-document` if the learnings imply documentation drift.",
		Description: "Capture learnings from completed work — decisions that held, assumptions that broke.",
	},
	{
		Slug: "okt-config",
		// okt-config is the system-tier orientation command for customizing the
		// user's environment/config. KEPT from the v1 surface (slug unchanged, no
		// deprecation) — refreshed for the v2 command surface so the next-move
		// hints point at the current orchestrator/granular tiers (e.g. okt-help for
		// the broader tour) rather than only the old implement loop.
		Action: "Orient the user on the active config layout so they can customize their omakiten environment. " +
			"Call `templates.show config-orientation` to load the path resolution order, entity layout, frontmatter " +
			"shapes, wiring relationships, and workflow guard kinds. Read it fully before answering any config-edit " +
			"question — do not guess. The config is where the user tailors omakiten: the active preset and workflow, the " +
			"personas/laws/skills/templates each `okt-*` command binds, the agent output language, and the workflow guard " +
			"rules. Editing an entity file or `omakiten.yaml` reshapes how every command resolves, so locate the exact " +
			"file before proposing a change. " +
			"Next: for the broader tour of how the command tiers fit together, suggest `okt-help`; when the user has a " +
			"concrete edit in mind, suggest `okt-task-implement` with the change scoped to `omakiten.yaml` or the " +
			"relevant entity file.",
		Description: "System command — orient on the active Omakiten config layout to customize the environment.",
	},
	{
		Slug: "okt-skill",
		// okt-skill is the system-tier command that wires UX onto the read-only
		// skills.list / skills.get MCP tools from CW6. Bare `/okt-skill` lists the
		// catalog via skills.list; `/okt-skill <slug>` loads one skill body via
		// skills.get. It pulls ANY skill in the catalog — it is NOT gated by the
		// active persona's skill repertoire (that gating only governs which skills
		// auto-flow into a command prompt; this command is the explicit escape
		// hatch to read any skill on demand). The load-bearing phrases (skills.get
		// for one body, skills.list for the catalog, ungated-by-repertoire) are
		// pinned by the CW7 okt-skill smoke test in agentruntime.
		Action: "Load a skill on demand, or browse the skill catalog. Resolve the slug from `--slug` or the first " +
			"positional argument (e.g. `/okt-skill commit`). " +
			"WITH A SLUG: call `skills.get` for that slug and surface the skill's full BODY verbatim — the procedural " +
			"payload the user asked to read (e.g. `/okt-skill commit` loads the `commit` skill body via `skills.get`). " +
			"When the slug is unknown, `skills.get` rejects naming the missing slug — relay that and suggest a bare " +
			"`okt-skill` to see the valid slugs. " +
			"WITH NO ARGUMENT: call `skills.list` and render the catalog — every loaded skill's slug + name + " +
			"description, ordered by slug — so the user can pick one to load. " +
			"This command pulls ANY skill in the catalog: it is NOT gated by the active persona's skill repertoire. The " +
			"repertoire only decides which skills auto-flow into a command's prompt; `okt-skill` is the explicit escape " +
			"hatch to read any authored skill on demand, regardless of which persona is bound. Read-only — skills are " +
			"authored by the user; never create, edit, or delete a skill through this command. " +
			"Next: when the loaded skill names a process step, suggest the matching granular command (e.g. the `commit` " +
			"skill → `okt-task-commit`); otherwise suggest `okt-help` for the command tour.",
		Description: "System command — load a skill body via skills.get (e.g. okt-skill commit), or list the catalog via skills.list with no arg; pulls any skill, ungated by persona repertoire.",
	},
	{
		Slug: "okt-task-commit",
		Action: "Draft Conventional Commits for the working tree. Read `git status` and `git diff --cached` " +
			"(fall back to unstaged changes when nothing is staged). Group hunks into one intent per commit; split " +
			"mixed trees via non-interactive staging (`git add <path>` / `git restore --staged <path>`). Derive the " +
			"scope from the touched paths. Draft `<type>(<scope>): <subject>` (≤50 chars, imperative) plus an " +
			"optional 72-column body that explains the \"why\" the diff does not. Surface every draft to the user " +
			"before invoking `git commit` via Bash. Never `git push` — the human owns publication. " +
			"Next: when the working tree is clean, suggest the user `git push` when ready.",
		Description: "Draft Conventional Commits for the working tree without pushing.",
	},
	{
		Slug: "okt-task-review",
		Action: "Walk the diff with the loaded lens. Run `git diff <base>..HEAD` (default base `main`; use " +
			"staged when explicit) and read every hunk before writing findings. Order the pass correctness → " +
			"security → smells → refactor opportunities → scalability/performance. Cite methodology by name when " +
			"applicable (`Extract Function — Fowler`, `Feature Envy — Fowler/Beck`, `Sprout Method — Feathers`, " +
			"`OCP — Martin`). Tag every finding by severity (`error` / `warning` / `info`). Call " +
			"`templates.show comment-review-findings` and `templates.show comment-refactor-opportunities` for " +
			"the scaffolds, then post the filled comments on the task. Read-only — never edit files, never run " +
			"`git commit`. Next: when findings need fixes, suggest `okt-task-implement` with the finding ids.",
		Description: "Walk the diff through Fowler/Beck/Martin/Feathers lens and surface findings + refactor opportunities.",
	},
	{
		Slug: "okt-task-secure",
		Action: "Walk the diff through a security lens. Trace untrusted input to sinks, check authz on every " +
			"new path, look for injection / SSRF / secret leakage / unsafe deserialization, and verify error paths do " +
			"not leak internals. Call `templates.show` for any bound findings scaffold and fill it. Cite the class of " +
			"each finding and tag it by severity; read-only — never edit. " +
			"Distinct from the general `okt-task-review`: this pass is security-only. Next: route findings to " +
			"`okt-task-implement` with the vulnerability class and location.",
		Description: "Security-only diff pass — input-to-sink tracing, authz, injection, secret leakage.",
	},
	{
		Slug: "okt-task-check",
		Action: "Run the project's check targets. Discover them via `mise tasks` first; fall back to " +
			"`npm run`, `make -qp`, `package.json > scripts`, or the repo's `CONTRIBUTING.md` — stop at the " +
			"first hit, do not guess. Invoke each target via Bash, capture stdout/stderr/exit code. Call " +
			"`templates.show comment-check-report` for the scaffold, then fill it — one row per target with " +
			"status (`pass` / `fail` / `skip` / `yellow`) and a one-line failing tail. Quote the last ≤10 " +
			"lines of stderr verbatim per failed target; never summarize errors. Read-only — never apply fixes, " +
			"never re-run after editing. Next: failures route to `okt-task-implement` with the target name + tail; " +
			"smell-level findings route to `okt-task-review` for triage.",
		Description: "Run discovered test/lint targets and report pass/fail in a tabular comment.",
	},
	{
		Slug: "okt-task-quality",
		Action: "Assess quality through a human lens — the judgment a linter cannot make. Read the diff for " +
			"design coherence, naming, test coverage of the meaningful branches, and the smells that pass the " +
			"mechanical gate but still erode the codebase. Call `templates.show` for any bound findings scaffold and " +
			"fill it. Distinct from the pass/fail `okt-task-check` mechanical " +
			"gate — this is the qualitative read. Surface findings by severity; read-only. Next: route structural " +
			"findings to `okt-task-refactor`, behavioral gaps to `okt-task-implement`.",
		Description: "Qualitative human-lens quality read — smells, coverage, design — distinct from the mechanical gate.",
	},
	{
		Slug: "okt-audit",
		// okt-audit is the Owner assurance orchestrator: like okt-run it is a PROMPT
		// the consuming agent acts on — omakiten cannot spawn agents itself — so the
		// action text encodes the playbook that instructs the agent to spawn the
		// Reviewer/Security subagents and aggregate their severity-tagged findings.
		// The load-bearing phrases below (spawn Reviewer + Security subagents via the
		// Agent tool; review → secure → quality → debrief; aggregate severity-tagged
		// findings; coach on severity/risk) are pinned by the okt-audit smoke test in
		// agentruntime.
		Action: "Commission an assurance pass on completed work. You are the director: you do not perform the " +
			"review yourself — you SPAWN SUBAGENTS via the Agent tool, one per assurance lens, and aggregate what they " +
			"return. Detect the target from context: a task id audits that task's diff, a plan id audits every task the " +
			"plan completed; a bare invocation audits the current branch's diff resolved via `project.overview` / " +
			"`plans.continue`. " +
			"SPAWN A REVIEWER SUBAGENT and a SECURITY SUBAGENT in parallel — their surfaces are disjoint, so concurrency " +
			"is worthwhile. The delegation contract you hand each is lean: it names the target and instructs the subagent " +
			"to INVOKE THE GRANULAR COMMANDS ITSELF via its OWN MCP access in its OWN FRESH CONTEXT — the Reviewer runs " +
			"`okt-task-review` then `okt-task-quality`; the Security subagent runs `okt-task-secure`. You NEVER render or " +
			"hold those command bodies; each subagent fetches its own. " +
			"RUN THE PLAYBOOK review → secure → quality → debrief: the review and secure passes run inside the spawned " +
			"subagents; once their findings land you commission the quality read and close with `okt-task-debrief` to " +
			"capture what the audit learned. " +
			"AGGREGATE THE FINDINGS into one report, each finding SEVERITY-TAGGED (`error` / `warning` / `info`) and " +
			"attributed to its lens; de-duplicate overlaps where the Reviewer and Security subagent flagged the same " +
			"line. COACH ON SEVERITY AND RISK: rank by blast radius, not count — one `error` on an auth path outweighs a " +
			"dozen `info` smells; call out which findings block ship versus which are follow-ups, and say so plainly. " +
			"This is the deep third-party review pass `okt-run` deliberately does NOT do — do not collapse it back into a " +
			"director acceptance gate. " +
			"Next: route blocking findings to `okt-task-implement` with the finding id and location, or suggest " +
			"`okt-pause` to record a handoff note when the audit clears.",
		Description: "Owner orchestrator — commission a deep assurance pass: spawn Reviewer + Security subagents, aggregate severity-tagged findings, coach on risk.",
	},
	{
		Slug: "okt-pause",
		Action: "Close the current session by snapshotting where the work stands into a handoff note the next " +
			"session reads first. Capture the live picture across all three planes: the GIT state (run `git status` and " +
			"`git diff --stat` via Bash for the working-tree summary, the current branch, and uncommitted work), the " +
			"ACTIVE TASK (`tasks.list` for in-flight ids, `task.activity.list` for what moved since the previous " +
			"handoff), and the PLAN (`plans.continue` / `plans.show` for the active wave and what remains claimable). " +
			"Synthesise material state since the previous handoff via `project.overview`. " +
			"Call `templates.show note-handoff` to fetch the scaffold, fill the populated slots, and PERSIST THE " +
			"HANDOFF via `comments.add` with `scope=project`, `kind=handoff` (no `task_id`) — the durable artifact is " +
			"the handoff comment, not the chat. Honor `--body` to override the rendered body verbatim and `--note` to append extra context " +
			"under a free-form section. When nothing material changed since the last handoff, render with a \"no " +
			"material changes since <prev>\" marker and still persist so the timeline stays continuous. " +
			"COACH THE HANDOFF QUALITY: lead with the single next action the next session should take, then the open " +
			"questions and the in-flight diff — write what an agent with zero context needs to resume, not a changelog " +
			"of what you did. When the cwd resolves no project, stop with `no project at <cwd>` and suggest " +
			"`--project <slug>`; when the project lacks an active workflow, omit the workflow/wave sections. Next: " +
			"suggest the user run `okt-start` (or `okt-note-recap`) at the top of their next session to load this " +
			"handoff back into context.",
		Description: "Concierge close — snapshot git + active task + plan into a handoff note for the next session.",
	},
	{
		Slug: "okt-note-free",
		Action: "Capture a free-form knowledge note without ceremony. Resolve scope from `--scope` " +
			"(default `project` when the cwd resolves; explicit `--scope global` always wins). Resolve kind " +
			"from `--kind` (default `free`); reject `handoff`, `standup-digest`, and `recap` here — those " +
			"belong to their dedicated commands. Title from `--title`; body from prompt or stdin. Call " +
			"`templates.show note-free` to fetch the minimal scaffold, then persist via `comments.add` with the " +
			"resolved scope (`project`, or `universal` for `--scope global`) and no `task_id`. " +
			"Reject empty body or empty title; when the cwd is ambiguous (multiple projects resolve) require " +
			"`--project <slug>`. Next: suggest `okt-note-list` to confirm the note landed, or `okt-note-recap` to " +
			"see it folded into the project timeline.",
		Description: "Capture a free-form knowledge note (project or global) without ceremony.",
	},
	{
		Slug: "okt-note-recap",
		Action: "Render a recap timeline of recent activity, scoped by the window argument. With a " +
			"single-project window (default — project from `--project <slug>` or cwd) it groups recent notes " +
			"chronologically: resolve the window from `[janela]`/`--since` (default `7d`) and the kind filter " +
			"from `--kinds <comma-list>` (default all), call `comments.list` with `scope=project` for the project " +
			"filtered by `since` (the window) and `kind`, then `templates.show note-recap` to fetch the scaffold; group entries by kind, order " +
			"chronologically with a timestamp prefix per bullet. With a wide window (`okt-note-recap day` or " +
			"any cross-project invocation where `--project` is omitted) it folds in the former standup digest: " +
			"enumerate every project the user owns, call `comments.list` with `scope=project` per project filtered by `kind=handoff` " +
			"and `since` (the window; per-project limit from `--limit`, default `5`), then `templates.show " +
			"note-standup-digest` and fill one section per project ordered by most recent handoff first, " +
			"silent projects last under a clear header; when more than 50 projects resolve, paginate or " +
			"require `--project`. Read-only either way — never persist; the recap is the artifact. When zero " +
			"notes/handoffs match the window, surface \"nothing in window\" (or \"no handoffs — run okt-pause\" " +
			"for the cross-project case). Next: suggest `okt-task-continue` with a specific task id when the " +
			"recap reveals an open thread to resume.",
		Description: "Recap timeline of recent notes; wide window folds in the cross-project handoff digest.",
	},
	{
		Slug: "okt-note-list",
		Action: "List knowledge notes for the active scope. Resolve scope from `--scope` (default both " +
			"project-scoped and universal notes when a project resolves; `project`/`global` narrow it, mapping " +
			"`global` to the `universal` comment scope), with optional " +
			"`--kind`, `--tag`, `--pinned`. Call `comments.list` with the " +
			"filters and report each note's id, kind, title, scope, and pinned flag — order pinned first, then most " +
			"recently updated. Read-only. Next: suggest `okt-note-show` with a note id to read one in full.",
		Description: "List knowledge notes for the active scope with kind/tag/pinned filters.",
	},
	{
		Slug: "okt-note-show",
		Action: "Read one note in full. Resolve the id from `--id` (or the first positional argument), call " +
			"`comments.list` (scoped, then match the row by comment id), and render the note's title, kind, scope, " +
			"tags, and body verbatim. Read-only — never mutate " +
			"here; `comments.edit`/`comments.delete` are MCP-only by design. Next: suggest `okt-note-list` to scan the " +
			"surrounding notes, or `okt-task-continue` when the note points at an open task to resume.",
		Description: "Read one knowledge note in full by id.",
	},
}

// commandBySlug indexes commandTable by slug for O(1) action/description
// lookup. Built once at package init from the single table so it never drifts.
var commandBySlug = func() map[string]commandEntry {
	out := make(map[string]commandEntry, len(commandTable))
	for _, e := range commandTable {
		out[e.Slug] = e
	}
	return out
}()

// CommandNames returns the canonical, ordered list of `okt-*` prompts the MCP
// adapter exposes. Order mirrors the REST-style handoff cycle so prompts/list
// answers in the order a user would naturally invoke them. It projects the
// Slug column of commandTable, the single source of truth.
func CommandNames() []string {
	out := make([]string, 0, len(commandTable))
	for _, e := range commandTable {
		out = append(out, e.Slug)
	}
	return out
}

// CommandDescription returns the prompts/list description for a known command,
// or the empty string when the command name is unknown.
func CommandDescription(name string) string {
	return commandBySlug[strings.TrimSpace(name)].Description
}

// CommandActionFallback returns the bare action text for `name` without any
// persona/laws/templates wiring. The MCP adapter falls back to this when no
// service is wired (test harnesses, partially initialized runtimes), so a
// prompts/get call still produces a usable message even before bindings are
// available.
func CommandActionFallback(name string) string {
	return commandBySlug[strings.TrimSpace(name)].Action
}
