package agentruntime

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/agent"
)

// promptBudgets caps each `okt-*` prompt's resolved markdown size in bytes,
// measured against the embedded default kit. It is a regression guardrail
// against silent prompt bloat: once a prompt breaks past its budget the test
// fails and forces a deliberate decision — trim the entity bodies, add a new
// optimization (e.g. JIT template bodies), or raise the budget with a
// justification in the same commit.
//
// CALIBRATION CONVENTION (the single rule this file follows):
//
//	budget = round-up-to-nearest-100( rendered_bytes × 1.3 )
//
// where `rendered_bytes` is the command's current resolved-markdown size
// against the canonical omakase kit, as emitted by `mise run mcp:prompts`. The
// ×1.3 factor leaves ~30% headroom so legitimate authoring growth (a longer
// persona body, an added law) is absorbed without a budget edit, while a
// runaway regression (a full skill repertoire leaking back in, a duplicated
// section) still trips the gate. Every value below is derived by this formula —
// no hand-tuned exceptions — so re-running the calibration after any kit change
// is mechanical: render, multiply, round, paste.
//
// History: skills render bullet-with-body since CW1 (#372); the 33 default
// skills gained full procedural bodies in W2 (#270); omakase wired command-level
// minimal skill SUBSETS (2-4 skills per command instead of the full persona
// repertoire) in W4 (#274). #379 (CW8) is the formal final pass: it freezes the
// full 40-command surface — orchestrators (okt, okt-start, okt-shape, okt-run,
// okt-audit, okt-pause), system (okt-help, okt-config, okt-skill), and the
// granular okt-task-* / okt-plan-* / okt-project-* / okt-note-* set.
//
// CW8 also closed a wiring gap that the W4 theming had left dangling: the
// per-command `skills:` subset authored in the omakase YAML was validated but
// never reached the resolver, so ResolveCommand fell back to the v2 personas'
// (empty) legacy `skills` field and rendered NO `## Skills` section at all. With
// the subset now flowing through (MCPCommandBinding.Skills →
// pickSkills), every command's declared skills render bullet-with-body inline —
// which is exactly the footprint these budgets must cover. That is why every
// skill-bearing command roughly doubled here versus the pre-fix numbers: the
// skill bodies are now actually present. Each value below is the post-fix
// rendered size × 1.3, rounded up to the nearest 100, against the canonical
// omakase kit. Cross-preset note: omakase is the only preset wiring command-level
// skill subsets today; others still flow the persona repertoire, but the gate
// runs against the omakase kit. When W6 themes the rest, re-run the formula.
//
// ENTITY-SOURCED PLAYBOOK BIND (mcp-prompts-entity-sourced, Wave 2): every
// command now also binds its okt-<slug>-playbook skill, so the orchestrator/
// system prompts that carry the largest playbook bodies (okt, okt-help,
// okt-run, okt-audit) grew past their prior budgets. The four values below are
// recalibrated by the same formula (rendered × 1.3, round up to nearest 100)
// against the post-bind omakase kit. These are transitional: the sibling
// follow-up that strips the now-duplicated Go Action/Description prose from the
// render path will reclaim that footprint, at which point the formula should be
// re-run and these four values re-tightened.
var promptBudgets = map[string]int{
	"okt":                   9600,
	"okt-help":              11400,
	"okt-start":             9300,
	"okt-shape":             10400,
	"okt-run":               15800,
	"okt-task-imagine":      8500,
	"okt-task-research":     4900,
	"okt-task-validate":     4800,
	"okt-task-requirements": 9000,
	"okt-task-prioritize":   9200,
	"okt-task-create":       14100,
	"okt-task-decompose":    6500,
	"okt-task-estimate":     6500,
	"okt-task-design":       4500,
	"okt-project-resume":    5300,
	"okt-project-continue":  5800,
	"okt-plan-create":       7000,
	"okt-plan-show":         7000,
	"okt-plan-continue":     7000,
	"okt-plan-claim":        7100,
	"okt-task-resume":       5400,
	"okt-task-continue":     5100,
	"okt-task-implement":    17400,
	"okt-task-self-review":  6300,
	"okt-task-refactor":     6600,
	"okt-task-document":     8300,
	"okt-task-debrief":      6700,
	"okt-config":            6800,
	"okt-skill":             7000,
	"okt-task-commit":       6300,
	"okt-task-review":       10000,
	"okt-task-secure":       6800,
	"okt-task-check":        7800,
	"okt-task-quality":      6700,
	"okt-audit":             12300,
	"okt-pause":             9100,
	"okt-note-free":         6500,
	"okt-note-recap":        11200,
	"okt-note-list":         6100,
	"okt-note-show":         5900,
}

// TestTemplateBoundCommandsCarryFetchHint guards the JIT contract for
// templates against the embedded default kit: every `okt-*` prompt that
// binds at least one template must surface the `templates.show` fetch hint
// somewhere in its rendered Markdown — typically via the action text
// (e.g. `okt-task-create`, `okt-config`) or the persona body (the bound
// persona's implement loop covers `okt-task-implement`). Without the hint, the agent has no
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

// orchestratorCommands is the bare-verb orchestrator tier of the v2 surface.
// The full-surface smoke gate asserts these additionally carry their
// guidance/suggestion block (a non-empty Action that names a downstream
// command), on top of the per-command section contract every command must meet.
var orchestratorCommands = map[string]struct{}{
	"okt":       {},
	"okt-start": {},
	"okt-shape": {},
	"okt-run":   {},
	"okt-audit": {},
	"okt-pause": {},
}

// TestFullCommandSurfaceSmoke is the AC#2 closeout gate: it renders EVERY
// command in agent.CommandNames() against the canonical omakase kit and asserts
// each one carries its expected sections. This is the consolidation of the
// per-wave CW3-CW7 subset gates (the prior TestCW5/TestCW6 *RenderNonEmpty
// tests) into one coherent full-surface contract so a command that registers
// but is left unwired — or a renderer regression that drops a section — fails
// here regardless of which wave introduced it.
//
// Every command must surface:
//   - a wired Persona section (role slot bound in the preset YAML),
//   - a non-empty Action section + non-empty Action field,
//   - a prompts/list description,
//   - a Laws section (the global law floor reaches every command),
//   - bullet-with-body Skills (the command's declared skill subset renders,
//     each as a `- **Name** — body` bullet — the W4 theming contract),
//   - a Templates section iff the command binds templates (and when it does,
//     the templates.show JIT fetch hint is present — see
//     TestTemplateBoundCommandsCarryFetchHint for the dedicated guard).
//
// Orchestrators additionally assert their guidance/suggestion block: the Action
// must name at least one downstream `okt-` command so the director hands off.
//
// Deeper per-command contracts (the guiding-playbook phrase pins for the
// orchestrators, the system-tier pins, the okt-run delegation playbook, and the
// distinct-pair assertions) live in the dedicated tests below; this gate is the
// breadth pass that guarantees no command in the surface renders empty.
func TestFullCommandSurfaceSmoke(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "data", "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")

	rt, err := Open(ctx, Options{DBPath: dbPath, ConfigPath: configPath, CWD: tmp})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = rt.Close() }()

	names := agent.CommandNames()
	if len(names) != 40 {
		t.Fatalf("expected the v2 surface to carry 40 commands, got %d — update this gate and the docs if the surface changed deliberately", len(names))
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			resp, err := rt.Service().ResolveCommand(ctx, agent.ResolveCommandInput{Name: name})
			if err != nil {
				t.Fatalf("ResolveCommand(%s) error = %v", name, err)
			}

			// Persona — role slot wired.
			if resp.Persona == nil {
				t.Fatalf("%s resolved with no persona — the role slot is not wired in the preset YAML", name)
			}
			if !strings.Contains(resp.Markdown, "## Persona — ") {
				t.Fatalf("%s markdown missing non-empty Persona section:\n%s", name, resp.Markdown)
			}

			// Action — non-empty section + field.
			if !strings.Contains(resp.Markdown, "## Action\n") || strings.TrimSpace(resp.Action) == "" {
				t.Fatalf("%s markdown missing non-empty Action section:\n%s", name, resp.Markdown)
			}

			// prompts/list description.
			if strings.TrimSpace(agent.CommandDescription(name)) == "" {
				t.Fatalf("%s carries no prompts/list description", name)
			}

			// Laws — the global floor reaches every command.
			if !strings.Contains(resp.Markdown, "## Laws\n") || len(resp.Laws) == 0 {
				t.Fatalf("%s markdown missing non-empty Laws section (the global law floor should reach every command):\n%s", name, resp.Markdown)
			}

			// Skills — bullet-with-body. Every v2 command declares a minimal
			// skill subset; each skill must render as a `- **Name** — body`
			// bullet under `## Skills`, never an empty section or a bare name.
			if len(resp.Skills) == 0 {
				t.Fatalf("%s resolved with no skills — the command-level skill subset is not wired (or the persona repertoire is empty)", name)
			}
			if !strings.Contains(resp.Markdown, "## Skills\n") {
				t.Fatalf("%s markdown missing the Skills section despite %d resolved skills:\n%s", name, len(resp.Skills), resp.Markdown)
			}
			for _, sk := range resp.Skills {
				label := sk.Name
				if label == "" {
					label = sk.Slug
				}
				body := strings.TrimSpace(sk.Body)
				if body == "" {
					body = strings.TrimSpace(sk.Description)
				}
				if body == "" {
					t.Fatalf("%s skill %q renders as a bare name bullet — bullet-with-body requires a non-empty body or description", name, label)
				}
				// The head of the bullet must be present verbatim in the
				// rendered markdown so we know the body actually shipped.
				head := body
				if idx := strings.IndexByte(head, '\n'); idx >= 0 {
					head = head[:idx]
				}
				wantBullet := "- **" + label + "** — " + head
				if !strings.Contains(resp.Markdown, wantBullet) {
					t.Fatalf("%s skill %q did not render bullet-with-body (expected line %q):\n%s", name, label, wantBullet, resp.Markdown)
				}
			}

			// Templates — section present iff bound, with the JIT fetch hint.
			if len(resp.Templates) > 0 {
				if !strings.Contains(resp.Markdown, "## Templates\n") {
					t.Fatalf("%s binds %d template(s) but renders no Templates section:\n%s", name, len(resp.Templates), resp.Markdown)
				}
				if !strings.Contains(resp.Markdown, "templates.show") {
					t.Fatalf("%s binds templates but carries no templates.show JIT fetch hint:\n%s", name, resp.Markdown)
				}
			}

			// Orchestrators carry a guidance/suggestion block: the Action must
			// hand off to at least one downstream okt- command.
			if _, isOrch := orchestratorCommands[name]; isOrch {
				if !strings.Contains(resp.Markdown, "okt-") {
					t.Fatalf("orchestrator %s carries no downstream okt- command suggestion in its guidance block:\n%s", name, resp.Markdown)
				}
				desc, ok := agent.DescribeCommand(name)
				if !ok || desc.Tier != agent.CommandTierOrchestrator {
					t.Fatalf("orchestrator %s must decode as the orchestrator tier, got %+v ok=%v", name, desc, ok)
				}
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
		// reads scope-aware comments (handoff + recap) so it resumes the prior thread
		"comments.list",
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
// current work across git + active task + plan and persists a project-scoped
// handoff via the scope-aware comments surface (comments.add, scope=project,
// kind=handoff). It guides the handoff quality and points at the next session's
// resume. Pinned below.
func TestCW4OktPauseHandoffNote(t *testing.T) {
	resp := resolveForSmoke(t, "okt-pause")
	assertGuidingOrchestrator(t, "okt-pause", resp, []string{
		// snapshot the three planes: git + active task + plan
		"git status",
		"active task",
		"task.activity.list",
		"plans.continue",
		// persists a project-scoped handoff via the comments surface
		"comments.add",
		"kind=handoff",
		"persist the handoff",
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
