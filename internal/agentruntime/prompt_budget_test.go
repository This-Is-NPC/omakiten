package agentruntime

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"omakiten/internal/commandcatalog"
	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/mcp"
)

// promptBudgets caps rendered prompt bytes against the bundled omakase kit.
var promptBudgets = map[string]int{
	"okt":                   7600,
	"okt-help":              8400,
	"okt-start":             9900,
	"okt-shape":             13800,
	"okt-run":               13400,
	"okt-task-imagine":      9100,
	"okt-task-research":     5500,
	"okt-task-validate":     5200,
	"okt-task-requirements": 9300,
	"okt-task-prioritize":   9400,
	"okt-task-create":       14900,
	"okt-task-decompose":    6800,
	"okt-task-estimate":     6900,
	"okt-task-design":       4800,
	"okt-project-resume":    5700,
	"okt-project-continue":  6100,
	"okt-plan-create":       7200,
	"okt-plan-show":         7200,
	"okt-plan-continue":     7300,
	"okt-plan-claim":        7300,
	"okt-task-resume":       5600,
	"okt-task-continue":     5300,
	"okt-task-implement":    17700,
	"okt-task-self-review":  6500,
	"okt-task-refactor":     6800,
	"okt-task-document":     8600,
	"okt-task-debrief":      6900,
	"okt-config":            6900,
	"okt-skill":             7200,
	"okt-task-commit":       6600,
	"okt-task-review":       12000,
	"okt-task-secure":       7000,
	"okt-task-check":        8100,
	"okt-task-quality":      6800,
	"okt-audit":             9900,
	"okt-pause":             9400,
	"okt-note-free":         7100,
	"okt-note-recap":        11900,
	"okt-note-list":         6500,
	"okt-note-show":         6500,
}

func TestPromptPlaybooksReferenceOnlyKnownMCPTools(t *testing.T) {
	resolve := commandResolver(t, "omakase")
	known := map[string]struct{}{}
	for _, tool := range mcp.Tools() {
		known[tool.Name] = struct{}{}
	}
	ignored := map[string]struct{}{"omakiten.yaml": {}, "package.json": {}, "task.assigned": {}}
	dottedBacktick := regexp.MustCompile("`([a-z_]+(?:\\.[a-z_]+)+)(?:\\s|`)")

	for _, name := range commandcatalog.CommandNames() {
		resp := resolve(t, name)
		matches := dottedBacktick.FindAllStringSubmatch(resp.Markdown, -1)
		for _, match := range matches {
			ref := match[1]
			if _, ok := ignored[ref]; ok {
				continue
			}
			if _, ok := known[ref]; !ok {
				t.Fatalf("%s prompt references unknown MCP tool %q:\n%s", name, ref, resp.Markdown)
			}
		}
	}
}

func assertFullCommandSurface(t *testing.T, name string, resp contract.ResolveCommandResponse) {
	t.Helper()
	assertCommandPersona(t, name, resp)
	assertCommandLaws(t, name, resp)
	assertCommandSkills(t, name, resp)
	assertCommandTemplates(t, name, resp)
	if desc, ok := commandcatalog.DescribeCommand(name); ok && desc.Tier == commandcatalog.CommandTierOrchestrator {
		assertOrchestratorGuidance(t, name, resp)
	}
}

func assertCommandPersona(t *testing.T, name string, resp contract.ResolveCommandResponse) {
	t.Helper()
	if resp.Persona == nil {
		t.Fatalf("%s resolved with no persona — the role slot is not wired in the preset YAML", name)
	}
	if !strings.Contains(resp.Markdown, "## Persona — ") {
		t.Fatalf("%s markdown missing non-empty Persona section:\n%s", name, resp.Markdown)
	}
}

func assertCommandLaws(t *testing.T, name string, resp contract.ResolveCommandResponse) {
	t.Helper()
	if !strings.Contains(resp.Markdown, "## Laws\n") || len(resp.Laws) == 0 {
		t.Fatalf("%s markdown missing non-empty Laws section (the global law floor should reach every command):\n%s", name, resp.Markdown)
	}
}

func assertCommandSkills(t *testing.T, name string, resp contract.ResolveCommandResponse) {
	t.Helper()
	if len(resp.Skills) == 0 {
		t.Fatalf("%s resolved with no skills — the command-level skill subset is not wired (or the persona repertoire is empty)", name)
	}
	if !strings.Contains(resp.Markdown, "## Skills\n") {
		t.Fatalf("%s markdown missing the Skills section despite %d resolved skills:\n%s", name, len(resp.Skills), resp.Markdown)
	}
	for _, sk := range resp.Skills {
		assertCommandSkillBullet(t, name, resp.Markdown, sk)
	}
}

func assertCommandSkillBullet(t *testing.T, name, markdown string, sk contract.SkillInfo) {
	t.Helper()
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
	head := body
	if idx := strings.IndexByte(head, '\n'); idx >= 0 {
		head = head[:idx]
	}
	wantBullet := "- **" + label + "** — " + head
	if !strings.Contains(markdown, wantBullet) {
		t.Fatalf("%s skill %q did not render bullet-with-body (expected line %q):\n%s", name, label, wantBullet, markdown)
	}
}

func assertCommandTemplates(t *testing.T, name string, resp contract.ResolveCommandResponse) {
	t.Helper()
	if len(resp.Templates) == 0 {
		return
	}
	if !strings.Contains(resp.Markdown, "## Templates\n") {
		t.Fatalf("%s binds %d template(s) but renders no Templates section:\n%s", name, len(resp.Templates), resp.Markdown)
	}
	if !strings.Contains(resp.Markdown, "templates.show") {
		t.Fatalf("%s binds templates but carries no templates.show JIT fetch hint:\n%s", name, resp.Markdown)
	}
}

func assertOrchestratorGuidance(t *testing.T, name string, resp contract.ResolveCommandResponse) {
	t.Helper()
	if !strings.Contains(resp.Markdown, "okt-") {
		t.Fatalf("orchestrator %s carries no downstream okt- command suggestion in its guidance block:\n%s", name, resp.Markdown)
	}
}

func TestDefaultKitCoversAllCommandsEntitySourced(t *testing.T) {
	names := commandcatalog.CommandNames()
	if len(names) == 0 {
		t.Fatal("command catalog is empty")
	}

	for _, preset := range config.ListPresets() {
		t.Run(preset.Name, func(t *testing.T) {
			assertPresetCommands(t, preset.Name, names)
		})
	}
}

func assertPresetCommands(t *testing.T, preset string, names []string) {
	t.Helper()
	resolve := commandResolver(t, preset)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			resp := resolve(t, name)
			assertEntitySourcedPrompt(t, preset, name, resp)
			assertFullCommandSurface(t, name, resp)
		})
	}
}

func assertEntitySourcedPrompt(t *testing.T, preset, name string, resp contract.ResolveCommandResponse) {
	t.Helper()
	if strings.TrimSpace(resp.Markdown) == "" {
		t.Fatalf("%s/%s resolved to an empty prompt", preset, name)
	}
	if strings.TrimSpace(resp.Description) == "" {
		t.Fatalf("%s/%s carries no prompts/list description — the bound okt-<slug>-playbook skill frontmatter did not flow through (the Go layer has no Description fallback)", preset, name)
	}
	wantPlaybook := name
	if wantPlaybook == "okt" {
		wantPlaybook = "okt-start"
	}
	wantPlaybook += "-playbook"
	playbook := findSkill(resp.Skills, wantPlaybook)
	if strings.TrimSpace(playbook.Body) == "" {
		t.Fatalf("%s/%s did not bind a non-empty %s playbook skill — the entity-sourced playbook is missing for this preset", preset, name, wantPlaybook)
	}
}

func findSkill(skills []contract.SkillInfo, slug string) contract.SkillInfo {
	for _, skill := range skills {
		if skill.Slug == slug {
			return skill
		}
	}
	return contract.SkillInfo{}
}

func commandResolver(t *testing.T, preset string) func(*testing.T, string) contract.ResolveCommandResponse {
	t.Helper()
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "data", "omakiten.db")
	configPath := filepath.Join(tmp, "config", preset+".yaml")

	rt, err := Open(ctx, Options{DBPath: dbPath, ConfigPath: configPath, CWD: tmp})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	return func(t *testing.T, name string) contract.ResolveCommandResponse {
		t.Helper()
		resp, err := rt.Service().ResolveCommand(ctx, contract.ResolveCommandInput{Name: name})
		if err != nil {
			t.Fatalf("ResolveCommand(%s) error = %v", name, err)
		}
		return resp
	}
}

// TestPromptBudgets renders every `okt-*` prompt against the embedded default
// kit and asserts each fits its byte budget. This is a regression guardrail
// against silent prompt bloat — adding a law to a global wiring or expanding a
// persona body without checking the impact would otherwise sneak in unnoticed.
func TestPromptBudgets(t *testing.T) {
	resolve := commandResolver(t, "omakase")

	for _, name := range commandcatalog.CommandNames() {
		t.Run(name, func(t *testing.T) {
			budget, ok := promptBudgets[name]
			if !ok {
				t.Fatalf("missing budget for prompt %q — add it to promptBudgets", name)
			}
			resp := resolve(t, name)
			size := len(resp.Markdown)
			if size > budget {
				t.Fatalf("prompt %s rendered to %d bytes, exceeds budget of %d (%.0f%% over). Trim the entity bodies, add a JIT optimization, or raise the budget with a justification in the same commit.\n\nRendered prompt:\n%s",
					name, size, budget, float64(size-budget)/float64(budget)*100, resp.Markdown)
			}
		})
	}
}
