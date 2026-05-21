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
var promptBudgets = map[string]int{
	"okt":           2300,
	"okt-imagine":   4900,
	"okt-create":    7600,
	"okt-resume":    2300,
	"okt-continue":  2400,
	"okt-implement": 8200,
	"okt-document":  2700,
	"okt-config":    3050,
	"okt-commit":    4700,
	"okt-review":    9000,
	"okt-check":     5500,
}

// TestTemplateBoundCommandsCarryFetchHint guards the JIT contract for
// templates against the embedded default kit: every `okt-*` prompt that
// binds at least one template must surface the `templates.show` fetch hint
// somewhere in its rendered Markdown — typically via the action text
// (e.g. `okt-create`, `okt-config`) or the persona body (engineer's implement
// loop covers `okt-implement`). Without the hint, the agent has no
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
