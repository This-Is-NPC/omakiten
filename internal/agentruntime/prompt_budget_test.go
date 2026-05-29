package agentruntime

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/agent"
)

// promptBudgets caps each `okt-*` prompt's resolved markdown size in bytes,
// measured against the embedded default kit. Values track the current default
// flow with ~30% headroom so legitimate growth is still allowed; once a prompt
// breaks past its budget the test fails and forces a deliberate decision —
// trim the entity bodies, add a new optimization (e.g. JIT persona body), or
// raise the budget with a justification in the same commit.
//
// Numbers come from `mise run mcp:prompts` output against `dev_env/`. Update
// alongside any change that grows a prompt past its current budget.
//
// Recalibrated in CW1 (#372): skills now render bullet-with-body (one bullet
// per skill carrying its body, or its description when body-less) instead of a
// single inline `## Skills — A, B` name list. Every skill-bearing prompt grew
// by the sum of its skills' description/body lines; budgets re-sized to the new
// default-kit footprint with ~30% headroom.
//
// Recalibrated post-W2 (#270): the 33 default skills gained full procedural
// bodies, so every skill-bearing command prompt grew again — the prior budgets
// were calibrated against body-less skill stubs. Budgets here reflect the
// new full-body-skill footprint: each is the command's current rendered size
// × ~1.3 (~30% headroom), rounded to the nearest 100, matching this file's
// convention. This is EXPECTED growth from the locked bullet-with-body design,
// not a regression. CW8 (#379) re-tightens these once theming rewires each
// command to a minimal skill subset rather than the full default repertoire.
// Recalibrated for omakase theming (#274 / W4): omakase became the first preset
// to wire command-level `skills:` — each command now selects a minimal SUBSET of
// its bound persona's skill_repertoire (2-4 skills) instead of letting the full
// repertoire flow through persona wiring. Every prompt shrank accordingly, so
// each budget below is the command's current rendered size × ~1.3 (~30%
// headroom), rounded to the nearest 100, matching this file's convention. This
// is the tightening the prior CW3-CW7 comments deferred to "#379"; #379 still
// owns the formal final pass across all presets, this wave just keeps the
// omakase kit honest and green now that its skill footprint is minimal.
var promptBudgets = map[string]int{
	"okt":                   4800,
	"okt-task-imagine":      4000,
	"okt-task-create":       8000,
	"okt-project-resume":    3100,
	"okt-task-continue":     2500,
	"okt-task-implement":    8000,
	"okt-task-document":     3300,
	"okt-config":            4200,
	"okt-task-commit":       5100,
	"okt-task-review":       5300,
	"okt-task-check":        4400,
	"okt-pause":             5900,
	"okt-note-free":         3900,
	"okt-note-recap":        5100,
	"okt-task-resume":       2800,
	"okt-task-research":     2600,
	"okt-task-validate":     2600,
	"okt-task-requirements": 4500,
	"okt-task-prioritize":   5100,
	"okt-task-decompose":    4400,
	"okt-task-estimate":     4300,
	"okt-task-design":       2700,
	"okt-task-self-review":  3800,
	"okt-task-refactor":     4300,
	"okt-task-quality":      4200,
	"okt-task-secure":       4100,
	"okt-task-debrief":      4100,
	"okt-plan-create":       4500,
	"okt-plan-show":         4500,
	"okt-plan-continue":     4500,
	"okt-plan-claim":        4600,
	"okt-project-continue":  3500,
	"okt-note-list":         3500,
	"okt-note-show":         3400,
	"okt-run":               7000,
	"okt-start":             4800,
	"okt-shape":             6500,
	"okt-audit":             6400,
	"okt-help":              5800,
	"okt-skill":             4500,
}

// TestTemplateBoundCommandsCarryFetchHint guards the JIT contract for
// templates against the embedded default kit: every `okt-*` prompt that
// binds at least one template must surface the `templates.show` fetch hint
// somewhere in its rendered Markdown — typically via the action text
// (e.g. `okt-task-create`, `okt-config`) or the persona body (engineer's
// implement loop covers `okt-task-implement`). Without the hint, the agent has no
// in-prompt anchor for the materialization step, which would defeat the JIT
// pattern.
func TestTemplateBoundCommandsCarryFetchHint(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "data", "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")

	rt, err := Open(ctx, Options{DBPath: dbPath, ConfigPath: configPath, CWD: tmp})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = rt.Close() }()

	for _, name := range agent.CommandNames() {
		t.Run(name, func(t *testing.T) {
			resp, err := rt.Service().ResolveCommand(ctx, agent.ResolveCommandInput{Name: name})
			if err != nil {
				t.Fatalf("ResolveCommand(%s) error = %v", name, err)
			}
			if len(resp.Templates) == 0 {
				t.Skip("command does not bind any templates")
			}
			if !strings.Contains(resp.Markdown, "templates.show") {
				t.Fatalf("prompt %s binds %d template(s) but the rendered Markdown carries no `templates.show` fetch hint — the agent has no in-prompt anchor for the JIT materialization step. Add the hint to the action text or to the bound persona's body.\n\nRendered prompt:\n%s",
					name, len(resp.Templates), resp.Markdown)
			}
		})
	}
}

// cw5GranularTaskCommands is the granular okt-task-* surface registered in
// CW5 (#376): the 12 new lifecycle commands plus the new cold-start nav.
var cw5GranularTaskCommands = []string{
	"okt-task-research",
	"okt-task-validate",
	"okt-task-requirements",
	"okt-task-prioritize",
	"okt-task-decompose",
	"okt-task-estimate",
	"okt-task-design",
	"okt-task-resume",
	"okt-task-self-review",
	"okt-task-refactor",
	"okt-task-quality",
	"okt-task-secure",
	"okt-task-debrief",
}

// TestCW5GranularCommandsRenderNonEmpty is the AC#5 smoke gate: every new
// granular okt-task-* command must resolve against the embedded default kit
// with a non-empty Persona section (the role slot is wired) and a non-empty
// Action section (the action text lands), and must surface a description in
// prompts/list metadata. A command that registers in CommandNames() but is
// not wired in the preset YAML would render with no Persona section, failing
// here.
func TestCW5GranularCommandsRenderNonEmpty(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "data", "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")

	rt, err := Open(ctx, Options{DBPath: dbPath, ConfigPath: configPath, CWD: tmp})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = rt.Close() }()

	for _, name := range cw5GranularTaskCommands {
		t.Run(name, func(t *testing.T) {
			resp, err := rt.Service().ResolveCommand(ctx, agent.ResolveCommandInput{Name: name})
			if err != nil {
				t.Fatalf("ResolveCommand(%s) error = %v", name, err)
			}
			if resp.Persona == nil {
				t.Fatalf("%s resolved with no persona — the role slot is not wired in the preset YAML", name)
			}
			if !strings.Contains(resp.Markdown, "## Persona — ") {
				t.Fatalf("%s markdown missing non-empty Persona section:\n%s", name, resp.Markdown)
			}
			if !strings.Contains(resp.Markdown, "## Action\n") || strings.TrimSpace(resp.Action) == "" {
				t.Fatalf("%s markdown missing non-empty Action section:\n%s", name, resp.Markdown)
			}
			if strings.TrimSpace(agent.CommandDescription(name)) == "" {
				t.Fatalf("%s carries no prompts/list description", name)
			}
		})
	}
}

// TestCW5DistinctCommandPairs pins the AC#2/#3/#4 distinctness contracts:
// cold-resume vs warm-continue, mechanical check vs human-lens quality, and
// author self-review vs third-party review must each carry materially
// different action text (not a copy-paste of the sibling).
func TestCW5DistinctCommandPairs(t *testing.T) {
	pairs := [][2]string{
		{"okt-task-resume", "okt-task-continue"},
		{"okt-task-check", "okt-task-quality"},
		{"okt-task-self-review", "okt-task-review"},
	}
	for _, p := range pairs {
		a := agent.CommandActionFallback(p[0])
		b := agent.CommandActionFallback(p[1])
		if a == "" || b == "" {
			t.Fatalf("action text missing for pair %s / %s", p[0], p[1])
		}
		if a == b {
			t.Fatalf("%s and %s share identical action text — they must behave distinctly", p[0], p[1])
		}
	}
	// Cold resume must signal full-context rebuild; warm continue must signal a
	// checkpoint read. Pin the load-bearing intent words so a future edit that
	// collapses the distinction surfaces here.
	resume := agent.CommandActionFallback("okt-task-resume")
	if !strings.Contains(strings.ToLower(resume), "cold") {
		t.Fatalf("okt-task-resume action should signal a cold full-context start:\n%s", resume)
	}
	cont := agent.CommandActionFallback("okt-task-continue")
	if !strings.Contains(strings.ToLower(cont), "checkpoint") {
		t.Fatalf("okt-task-continue action should signal a warm checkpoint read:\n%s", cont)
	}
}

// cw6GranularCommands is the granular plan/project/note surface registered in
// CW6 (#377): 4 plan commands, the new warm okt-project-continue, and the new
// okt-note-list / okt-note-show navigation pair.
var cw6GranularCommands = []string{
	"okt-plan-create",
	"okt-plan-show",
	"okt-plan-continue",
	"okt-plan-claim",
	"okt-project-continue",
	"okt-note-list",
	"okt-note-show",
}

// TestCW6GranularCommandsRenderNonEmpty is the AC#6 smoke gate (mirrors the
// CW5 gate): every new granular plan/project/note command must resolve against
// the embedded default kit with a non-empty Persona section (the role slot is
// wired in the preset YAML) and a non-empty Action section, and must surface a
// prompts/list description.
func TestCW6GranularCommandsRenderNonEmpty(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "data", "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")

	rt, err := Open(ctx, Options{DBPath: dbPath, ConfigPath: configPath, CWD: tmp})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = rt.Close() }()

	for _, name := range cw6GranularCommands {
		t.Run(name, func(t *testing.T) {
			resp, err := rt.Service().ResolveCommand(ctx, agent.ResolveCommandInput{Name: name})
			if err != nil {
				t.Fatalf("ResolveCommand(%s) error = %v", name, err)
			}
			if resp.Persona == nil {
				t.Fatalf("%s resolved with no persona — the role slot is not wired in the preset YAML", name)
			}
			if !strings.Contains(resp.Markdown, "## Persona — ") {
				t.Fatalf("%s markdown missing non-empty Persona section:\n%s", name, resp.Markdown)
			}
			if !strings.Contains(resp.Markdown, "## Action\n") || strings.TrimSpace(resp.Action) == "" {
				t.Fatalf("%s markdown missing non-empty Action section:\n%s", name, resp.Markdown)
			}
			if strings.TrimSpace(agent.CommandDescription(name)) == "" {
				t.Fatalf("%s carries no prompts/list description", name)
			}
		})
	}
}

// TestCW6ProjectResumeVsContinueDistinct pins AC#4: the cold-overview
// okt-project-resume and the warm last-session okt-project-continue must carry
// materially distinct action text. Resume signals a cold scan; continue signals
// a warm hand-back.
func TestCW6ProjectResumeVsContinueDistinct(t *testing.T) {
	resume := agent.CommandActionFallback("okt-project-resume")
	cont := agent.CommandActionFallback("okt-project-continue")
	if resume == "" || cont == "" {
		t.Fatalf("action text missing for okt-project-resume / okt-project-continue")
	}
	if resume == cont {
		t.Fatalf("okt-project-resume and okt-project-continue share identical action text — they must behave distinctly")
	}
	if !strings.Contains(strings.ToLower(cont), "warm") {
		t.Fatalf("okt-project-continue action should signal a warm last-session resume:\n%s", cont)
	}
}

// TestCW3OktRunDelegationPlaybook is the AC#8 smoke gate. omakiten cannot
// itself spawn agents — okt-run is a PROMPT, and the agent consuming it does
// the spawning — so the smoke test asserts the RENDERED okt-run prompt against
// the embedded default kit carries the locked Owner→Builder delegation
// playbook contract. Each assertion pins a load-bearing phrase so a future edit
// that erodes a clause of the contract surfaces here (mirrors the
// TestCW5DistinctCommandPairs phrase-pinning style).
func TestCW3OktRunDelegationPlaybook(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "data", "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")

	rt, err := Open(ctx, Options{DBPath: dbPath, ConfigPath: configPath, CWD: tmp})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = rt.Close() }()

	resp, err := rt.Service().ResolveCommand(ctx, agent.ResolveCommandInput{Name: "okt-run"})
	if err != nil {
		t.Fatalf("ResolveCommand(okt-run) error = %v", err)
	}

	// The role slot must be wired (Owner persona) and the action must land.
	if resp.Persona == nil {
		t.Fatalf("okt-run resolved with no persona — the Owner role slot is not wired in the preset YAML")
	}
	if !strings.Contains(resp.Markdown, "## Persona — ") {
		t.Fatalf("okt-run markdown missing non-empty Persona section:\n%s", resp.Markdown)
	}
	if !strings.Contains(resp.Markdown, "## Action\n") || strings.TrimSpace(resp.Action) == "" {
		t.Fatalf("okt-run markdown missing non-empty Action section:\n%s", resp.Markdown)
	}
	if strings.TrimSpace(agent.CommandDescription("okt-run")) == "" {
		t.Fatalf("okt-run carries no prompts/list description")
	}

	// okt-run decodes as a bare orchestrator (not granular) — the engine is
	// subagents, not a workflow.
	desc, ok := agent.DescribeCommand("okt-run")
	if !ok || desc.Tier != agent.CommandTierOrchestrator {
		t.Fatalf("okt-run must decode as the orchestrator tier, got %+v ok=%v", desc, ok)
	}

	lower := strings.ToLower(resp.Markdown)

	// Each clause of the locked delegation contract, pinned by a load-bearing
	// phrase. The contract: spawn ONE Builder subagent PER task via the Agent
	// tool; lean Owner context that NEVER holds okt-task-* bodies; the Builder
	// invokes the granular commands ITSELF via its own MCP in its own fresh
	// context; conditional parallelism gated on deps-satisfied AND worthwhile
	// (explicitly NOT parallelize-everything); a compact structured return
	// (diff + #tests-passing) the Owner reviews; a clean halt on failure that
	// leaves the run resumable; and review here is NOT the deep okt-audit pass.
	contract := []struct {
		clause string
		phrase string
	}{
		{"spawn one Builder subagent per task via the Agent tool", "spawn one builder subagent per task via the agent tool"},
		{"delegates implementation (Owner does not implement)", "you do not implement"},
		{"lean Owner context never holds okt-task-* bodies", "do not load the `okt-task-*` command bodies"},
		{"Builder invokes the granular commands itself via its own MCP", "invoke the granular `okt-task-*` commands itself"},
		{"Builder runs in its own fresh context", "own fresh context"},
		{"conditional parallelism gated on deps satisfied AND worthwhile", "only when their dependencies are satisfied and"},
		{"explicitly not parallelize-everything", "never parallelize everything"},
		{"tasks with unmet dependencies wait", "wait"},
		{"compact structured return for review", "compact, structured result"},
		{"return carries #tests-passing evidence", "#tests-passing"},
		{"Owner reviews the return: accept / reject / re-spawn", "re-spawn a fresh builder"},
		{"deep review lives in okt-audit, not duplicated here", "deep review lives in"},
		{"clean halt on a failing/blocked task", "halt cleanly"},
		{"run is resumable from the halted task", "resumable"},
		{"single-task or plan target detected from context", "detect the target from context"},
	}
	for _, c := range contract {
		if !strings.Contains(lower, c.phrase) {
			t.Fatalf("okt-run prompt missing the %q clause (expected load-bearing phrase %q):\n%s", c.clause, c.phrase, resp.Markdown)
		}
	}
}

// resolveForSmoke is a CW4 helper: it opens the runtime against the embedded
// omakase default kit and resolves one command, failing the test on any error.
// It centralises the boilerplate the four guiding-orchestrator smoke gates
// share, mirroring the single-command resolve the okt-run gate does inline.
func resolveForSmoke(t *testing.T, name string) agent.ResolveCommandResponse {
	t.Helper()
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "data", "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")

	rt, err := Open(ctx, Options{DBPath: dbPath, ConfigPath: configPath, CWD: tmp})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	resp, err := rt.Service().ResolveCommand(ctx, agent.ResolveCommandInput{Name: name})
	if err != nil {
		t.Fatalf("ResolveCommand(%s) error = %v", name, err)
	}
	return resp
}

// assertGuidingOrchestrator is the shared CW4 smoke assertion: the command must
// resolve as the orchestrator tier, carry a wired persona + non-empty action +
// prompts/list description, and its rendered prompt must contain every pinned
// load-bearing phrase (case-insensitive). Each phrase pins a clause of the
// guiding playbook so a future edit that erodes the next-move/coaching contract
// surfaces here — same phrase-pinning style as TestCW3OktRunDelegationPlaybook.
func assertGuidingOrchestrator(t *testing.T, name string, resp agent.ResolveCommandResponse, phrases []string) {
	t.Helper()
	if resp.Persona == nil {
		t.Fatalf("%s resolved with no persona — the role slot is not wired in the preset YAML", name)
	}
	if !strings.Contains(resp.Markdown, "## Persona — ") {
		t.Fatalf("%s markdown missing non-empty Persona section:\n%s", name, resp.Markdown)
	}
	if !strings.Contains(resp.Markdown, "## Action\n") || strings.TrimSpace(resp.Action) == "" {
		t.Fatalf("%s markdown missing non-empty Action section:\n%s", name, resp.Markdown)
	}
	if strings.TrimSpace(agent.CommandDescription(name)) == "" {
		t.Fatalf("%s carries no prompts/list description", name)
	}
	desc, ok := agent.DescribeCommand(name)
	if !ok || desc.Tier != agent.CommandTierOrchestrator {
		t.Fatalf("%s must decode as the orchestrator tier, got %+v ok=%v", name, desc, ok)
	}
	lower := strings.ToLower(resp.Markdown)
	for _, phrase := range phrases {
		if !strings.Contains(lower, strings.ToLower(phrase)) {
			t.Fatalf("%s prompt missing load-bearing phrase %q (the guiding playbook eroded):\n%s", name, phrase, resp.Markdown)
		}
	}
}

// TestCW4OktStartSmartEntry is the AC#2 smoke gate: okt-start is the Concierge
// smart entry that reads handoff/recap notes plus plan/board state and proposes
// concrete next commands — including the suggest-a-plan-when-tasks-but-no-plan
// branch — while teaching the available options. It is a PROMPT the agent acts
// on; the gate pins the load-bearing clauses in the rendered prompt.
func TestCW4OktStartSmartEntry(t *testing.T) {
	resp := resolveForSmoke(t, "okt-start")
	assertGuidingOrchestrator(t, "okt-start", resp, []string{
		// reads notes (handoff + recap) so it resumes the prior thread
		"notes.list",
		"handoff",
		"recap",
		// reads plan + board state
		"plans.list",
		"project.overview",
		"tasks.list",
		// proposes concrete next commands (names the actual command)
		"propose concrete next commands",
		"okt-task-continue",
		"okt-plan-continue",
		// suggest a plan when the board has tasks but no plan
		"the board has tasks but no plan",
		// teaches the available options — guiding, not execute-only
		"teach the available options",
		"the entry coaches, it does not just",
	})
}

// TestCW4OktStartIsOktShortcut is the AC#6 gate: the bare `okt` must resolve to
// the SAME smart-entry playbook as `okt-start`. Both keys point at the shared
// oktStartAction const, so their resolved action text is byte-identical — the
// resolution-level shortcut with no string-alias table.
func TestCW4OktStartIsOktShortcut(t *testing.T) {
	okt := resolveForSmoke(t, "okt")
	start := resolveForSmoke(t, "okt-start")
	if strings.TrimSpace(okt.Action) == "" {
		t.Fatal("okt resolved with empty action text")
	}
	if okt.Action != start.Action {
		t.Fatalf("`okt` does not shortcut to `okt-start` — action texts diverge.\nokt:\n%s\n\nokt-start:\n%s", okt.Action, start.Action)
	}
	// The shortcut must still carry the smart-entry contract, not the old thin
	// router — pin the proposes-next-commands clause on the `okt` resolution.
	if !strings.Contains(strings.ToLower(okt.Markdown), "propose concrete next commands") {
		t.Fatalf("`okt` shortcut lost the smart-entry playbook:\n%s", okt.Markdown)
	}
}

// TestCW4OktShapeChainsGranulars is the AC#3 smoke gate: okt-shape chains the
// discover/define granulars + okt-plan-create and surfaces what is still
// undefined. It directs by command NAME only — pinned below.
func TestCW4OktShapeChainsGranulars(t *testing.T) {
	resp := resolveForSmoke(t, "okt-shape")
	assertGuidingOrchestrator(t, "okt-shape", resp, []string{
		// chains the discover granulars
		"chain the discover",
		"okt-task-research",
		"okt-task-validate",
		// chains the define granulars
		"okt-task-requirements",
		"okt-task-prioritize",
		"okt-task-create",
		// produces an execution plan
		"okt-plan-create",
		"ordered waves",
		// surfaces gaps
		"surface what is still undefined",
		// guides the decision (coach the fork)
		"coach the decision",
		// next-move handoff
		"okt-run",
	})
}

// TestCW4OktAuditSpawnsSubagents is the AC#4 smoke gate: okt-audit is a PROMPT
// that instructs the consuming agent to spawn Reviewer + Security subagents and
// aggregate severity-tagged findings (omakiten cannot spawn agents itself). The
// gate pins the spawn contract, the review→secure→quality→debrief playbook, the
// severity-tagged aggregation, and the risk coaching.
func TestCW4OktAuditSpawnsSubagents(t *testing.T) {
	resp := resolveForSmoke(t, "okt-audit")
	assertGuidingOrchestrator(t, "okt-audit", resp, []string{
		// spawns Reviewer + Security subagents via the Agent tool
		"spawn subagents",
		"spawn a reviewer subagent",
		"security subagent",
		"the agent tool",
		// each subagent invokes the granular commands itself in its own context
		"own fresh context",
		"okt-task-review",
		"okt-task-secure",
		"okt-task-quality",
		// review → secure → quality → debrief playbook
		"review → secure → quality → debrief",
		"okt-task-debrief",
		// aggregates severity-tagged findings
		"aggregate the findings",
		"severity-tagged",
		// coaches on severity / risk
		"coach on severity and risk",
		// the deep pass okt-run deliberately does not do
		"deep third-party review pass",
	})
}

// TestCW4OktPauseHandoffNote is the AC#5 smoke gate: okt-pause snapshots the
// current work across git + active task + plan and produces a handoff note via
// the notes MCP (notes.create, kind=handoff — the CreateNote type handoff
// intent). It guides the handoff quality and points at the next session's
// resume. Pinned below.
func TestCW4OktPauseHandoffNote(t *testing.T) {
	resp := resolveForSmoke(t, "okt-pause")
	assertGuidingOrchestrator(t, "okt-pause", resp, []string{
		// snapshot the three planes: git + active task + plan
		"git status",
		"active task",
		"task.activity.list",
		"plans.continue",
		// produces a handoff note via the notes MCP (CreateNote type handoff)
		"notes.create",
		"kind=handoff",
		"persist the handoff note",
		// guides the handoff quality (next-move + coaching)
		"coach the handoff quality",
		"single next action",
		// next session resume handoff
		"okt-start",
	})
}

// assertSystemCommand is the shared CW7 smoke assertion: the command must
// resolve as the SYSTEM tier (talk to the tool, no project object), carry a
// wired persona + non-empty action + prompts/list description, and its rendered
// prompt must contain every pinned load-bearing phrase (case-insensitive).
// Mirrors assertGuidingOrchestrator but pins the system tier instead of the
// orchestrator tier.
func assertSystemCommand(t *testing.T, name string, resp agent.ResolveCommandResponse, phrases []string) {
	t.Helper()
	if resp.Persona == nil {
		t.Fatalf("%s resolved with no persona — the role slot is not wired in the preset YAML", name)
	}
	if !strings.Contains(resp.Markdown, "## Persona — ") {
		t.Fatalf("%s markdown missing non-empty Persona section:\n%s", name, resp.Markdown)
	}
	if !strings.Contains(resp.Markdown, "## Action\n") || strings.TrimSpace(resp.Action) == "" {
		t.Fatalf("%s markdown missing non-empty Action section:\n%s", name, resp.Markdown)
	}
	if strings.TrimSpace(agent.CommandDescription(name)) == "" {
		t.Fatalf("%s carries no prompts/list description", name)
	}
	desc, ok := agent.DescribeCommand(name)
	if !ok || desc.Tier != agent.CommandTierSystem {
		t.Fatalf("%s must decode as the system tier, got %+v ok=%v", name, desc, ok)
	}
	lower := strings.ToLower(resp.Markdown)
	for _, phrase := range phrases {
		if !strings.Contains(lower, strings.ToLower(phrase)) {
			t.Fatalf("%s prompt missing load-bearing phrase %q:\n%s", name, phrase, resp.Markdown)
		}
	}
}

// TestCW7OktHelpTierGuide is the AC#1 smoke gate: okt-help renders a tier-aware
// guide (orchestrators / system / granular), walks the start → shape → run →
// audit → pause mental flow, and gives the drop-to-granular decision hint. It
// is a PROMPT the agent acts on; the gate pins the load-bearing tier names,
// the flow command chain, and the decision hint — phrase-pinning style mirrors
// TestCW3OktRunDelegationPlaybook.
func TestCW7OktHelpTierGuide(t *testing.T) {
	resp := resolveForSmoke(t, "okt-help")
	assertSystemCommand(t, "okt-help", resp, []string{
		// the three command tiers, named
		"the command tiers",
		"orchestrators",
		"system",
		"granular",
		// the start → shape → run → audit → pause mental flow
		"the mental flow",
		"okt-start",
		"okt-shape",
		"okt-run",
		"okt-audit",
		"okt-pause",
		// when to drop to the granular okt-task-* / okt-plan-* surface
		"when to drop to granular",
		"okt-task-implement",
		"okt-plan-claim",
	})
}

// TestCW7OktConfigReachable is the AC#2 gate: okt-config is retained and
// reachable (registered in CommandNames, resolves with a wired persona,
// decodes as the system tier), and its orientation content is current with the
// v2 surface — it points at okt-help for the broader tour. It binds the
// config-orientation template, so the JIT fetch hint must be present.
func TestCW7OktConfigReachable(t *testing.T) {
	registered := false
	for _, n := range agent.CommandNames() {
		if n == "okt-config" {
			registered = true
			break
		}
	}
	if !registered {
		t.Fatal("okt-config is not registered in CommandNames() — the kept system command was dropped")
	}
	resp := resolveForSmoke(t, "okt-config")
	assertSystemCommand(t, "okt-config", resp, []string{
		// orients the user to customize their config/environment
		"templates.show config-orientation",
		"customize their omakiten environment",
		// content current with the v2 surface — points at the broader tour
		"okt-help",
	})
}

// TestCW7OktSkillWiredToSkillTools is the AC#3 gate: okt-skill's UX is wired
// onto the read-only skills.get / skills.list MCP tools from CW6 — a slug loads
// one skill body via skills.get (with `commit` named as the example), a bare
// invocation lists the catalog via skills.list, and the command pulls ANY skill
// ungated by the persona's skill repertoire.
func TestCW7OktSkillWiredToSkillTools(t *testing.T) {
	resp := resolveForSmoke(t, "okt-skill")
	assertSystemCommand(t, "okt-skill", resp, []string{
		// with a slug → skills.get for the body; commit is the named example
		"skills.get",
		"/okt-skill commit",
		"commit` skill body",
		// no arg → catalog via skills.list
		"skills.list",
		"render the catalog",
		// pulls any skill, ungated by the persona repertoire
		"any skill",
		"not gated by the active persona's skill repertoire",
	})
}

// TestPromptBudgets renders every `okt-*` prompt against the embedded default
// kit and asserts each fits its byte budget. This is a regression guardrail
// against silent prompt bloat — adding a law to a global wiring or expanding a
// persona body without checking the impact would otherwise sneak in unnoticed.
func TestPromptBudgets(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "data", "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")

	rt, err := Open(ctx, Options{DBPath: dbPath, ConfigPath: configPath, CWD: tmp})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = rt.Close() }()

	for _, name := range agent.CommandNames() {
		t.Run(name, func(t *testing.T) {
			budget, ok := promptBudgets[name]
			if !ok {
				t.Fatalf("missing budget for prompt %q — add it to promptBudgets", name)
			}
			resp, err := rt.Service().ResolveCommand(ctx, agent.ResolveCommandInput{Name: name})
			if err != nil {
				t.Fatalf("ResolveCommand(%s) error = %v", name, err)
			}
			size := len(resp.Markdown)
			if size > budget {
				t.Fatalf("prompt %s rendered to %d bytes, exceeds budget of %d (%.0f%% over). Trim the entity bodies, add a JIT optimization, or raise the budget with a justification in the same commit.\n\nRendered prompt:\n%s",
					name, size, budget, float64(size-budget)/float64(budget)*100, resp.Markdown)
			}
		})
	}
}
