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
var promptBudgets = map[string]int{
	"okt":                23000,
	"okt-task-imagine":   26400,
	"okt-task-create":    29900,
	"okt-project-resume": 22900,
	"okt-task-continue":  23100,
	"okt-task-implement": 28300,
	"okt-task-document":  20200,
	"okt-config":         20600,
	"okt-task-commit":    9500,
	"okt-task-review":    19400,
	"okt-task-check":     15200,
	// Notes/handoff commands (#363), rescoped to the v2 prefix surface
	// (#373). Budgets sized against the embedded omakase default kit with
	// ~30% headroom. okt-pause (former handoff) carries the longest action
	// body (workflow/wave edge cases) plus the `project-scope-only` +
	// `no-praise-pad` laws and the `note-handoff` template metadata.
	// okt-note-recap folds in the former standup digest: its action body is
	// the longest of the note family and it binds both note-recap and
	// note-standup-digest template metadata, so its budget is raised.
	"okt-pause":      21900,
	"okt-note-free":  20900,
	"okt-note-recap": 24900,
	// CW5 (#376): granular okt-task-* surface — 12 new lifecycle commands + 1
	// new nav (okt-task-resume). Budgets sized against the embedded omakase
	// default kit with ~30% headroom, bucketed by the bound persona's skill
	// footprint: Builder (engineer) ~ continue/implement, Owner (product-owner)
	// ~ imagine/create, Tester (check-runner) ~ check, Reviewer (code-reviewer)
	// ~ review, Scribe (documentation-agent) ~ document. CW8 (#379)
	// re-tightens these once theming rewires each command to a minimal skill
	// subset rather than the full default repertoire.
	"okt-task-resume":       23500,
	"okt-task-research":     27000,
	"okt-task-validate":     27000,
	"okt-task-requirements": 27000,
	"okt-task-prioritize":   27000,
	"okt-task-decompose":    23700,
	"okt-task-estimate":     23600,
	"okt-task-design":       23800,
	"okt-task-self-review":  23700,
	"okt-task-refactor":     23700,
	"okt-task-quality":      15600,
	"okt-task-secure":       19700,
	"okt-task-debrief":      20300,
	// CW6 (#377): granular plan/project/note surface. Budgets sized against the
	// embedded omakase default kit with ~30% headroom, bucketed by the bound
	// persona's skill footprint: okt-plan-* bind Owner (product-owner, ~ imagine
	// footprint); okt-project-continue and okt-note-list/show bind Builder
	// (engineer, ~ continue/note footprint). CW8 (#379) re-tightens once theming
	// rewires each command to a minimal skill subset.
	"okt-plan-create":      27000,
	"okt-plan-show":        27000,
	"okt-plan-continue":    27000,
	"okt-plan-claim":       27000,
	"okt-project-continue": 23500,
	"okt-note-list":        23500,
	"okt-note-show":        23500,
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
