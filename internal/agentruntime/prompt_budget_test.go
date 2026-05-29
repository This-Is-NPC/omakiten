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
