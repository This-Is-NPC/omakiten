package cli

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/installer"
)

// keyMsg builds a tea.KeyMsg from a printable rune so tests can read
// like "press 'j' then enter" without binding to specific key constants.
func keyMsg(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func enterMsg() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEnter} }
func ctrlCMsg() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyCtrlC} }
func downMsg() tea.KeyMsg  { return tea.KeyMsg{Type: tea.KeyDown} }
func spaceMsg() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeySpace} }

// stepThrough feeds messages one at a time so test assertions can
// inspect the model state between every keystroke; matches how the tea
// runtime would dispatch them but stays synchronous so tests do not
// have to spin a real program loop.
func stepThrough(t *testing.T, m setupPickerModel, msgs ...tea.Msg) setupPickerModel {
	t.Helper()
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		m = next.(setupPickerModel)
	}
	return m
}

// TestSetupPicker_HappyPath drives a fresh install through every screen
// (CLI=pt-br → TUI=pt-br → agent="Português (Brasil)" → preset=omakase →
// harnesses=claude-code+opencode), then asserts the resulting inputs
// match what runSetup would receive. This is the AC §12 "happy path"
// scenario the task contract calls out for teatest coverage; we drive
// the model functionally instead of via a real PTY because the project
// has no teatest dependency and the model's Update is the same code
// the tea.Program would dispatch.
func TestSetupPicker_HappyPath(t *testing.T) {
	needs := pickerNeeds{CLILang: true, TUILang: true, Agent: true, Preset: true, Harness: true}
	m, err := newSetupPickerModel(setupInputs{}, needs)
	if err != nil {
		t.Fatalf("newSetupPickerModel: %v", err)
	}
	if m.step != stepCLILang {
		t.Fatalf("step: got %v want stepCLILang", m.step)
	}

	// CLI lang — bundled order is alphabetical: en (0), pt-br (1). Press
	// down then enter to pick pt-br.
	m = stepThrough(t, m, downMsg(), enterMsg())
	if m.step != stepTUILang {
		t.Fatalf("after CLI confirm: step %v want stepTUILang", m.step)
	}
	if m.inputs.CLILang != "pt-br" {
		t.Fatalf("CLILang: got %q want pt-br", m.inputs.CLILang)
	}
	if m.cliCatalog == nil || m.cliCatalog.Code != "pt-br" {
		t.Fatalf("cliCatalog not loaded for pt-br: %+v", m.cliCatalog)
	}
	// TUI cursor should pre-select pt-br (same as CLI).
	if m.tuiCursor != 1 {
		t.Fatalf("TUI cursor: got %d want 1 (pt-br)", m.tuiCursor)
	}

	// TUI lang — accept default (pt-br).
	m = stepThrough(t, m, enterMsg())
	if m.step != stepAgentLang {
		t.Fatalf("after TUI confirm: step %v want stepAgentLang", m.step)
	}
	if m.inputs.TUILang != "pt-br" {
		t.Fatalf("TUILang: got %q want pt-br", m.inputs.TUILang)
	}

	// Agent lang — type "Portugues" and enter (agent input mirrors the
	// runes; we keep ASCII here so the test does not depend on the
	// terminal's input encoding).
	m = stepThrough(t, m,
		keyMsg('P'), keyMsg('o'), keyMsg('r'), keyMsg('t'), keyMsg('u'), keyMsg('g'), keyMsg('u'), keyMsg('e'), keyMsg('s'),
		enterMsg(),
	)
	if m.step != stepPreset {
		t.Fatalf("after agent confirm: step %v want stepPreset", m.step)
	}
	if m.inputs.AgentLang != "Portugues" {
		t.Fatalf("AgentLang: got %q want Portugues", m.inputs.AgentLang)
	}
	if !m.inputs.AgentLangSet {
		t.Fatalf("AgentLangSet must be true after enter")
	}
	if len(m.presets) == 0 {
		t.Fatalf("presets should be populated by transition into stepPreset")
	}

	// Preset — first row is omakase (alphabetical preset order would
	// place it second; we depend on installer.SupportedPresets() ordering
	// which is config.ListPresets ordering).
	presetIdx := -1
	for i, p := range m.presets {
		if p.Name == "omakase" {
			presetIdx = i
			break
		}
	}
	if presetIdx < 0 {
		t.Fatalf("omakase preset not found in %v", m.presets)
	}
	for m.presetCursor < presetIdx {
		m = stepThrough(t, m, downMsg())
	}
	m = stepThrough(t, m, enterMsg())
	if m.step != stepHarness {
		t.Fatalf("after preset confirm: step %v want stepHarness", m.step)
	}
	if m.inputs.Preset != "omakase" {
		t.Fatalf("Preset: got %q want omakase", m.inputs.Preset)
	}

	// Harness — toggle claude-code (index 0) and opencode then enter.
	supported := installer.SupportedHarnesses()
	want := []string{}
	for _, name := range []string{"claude-code", "opencode"} {
		for i, h := range supported {
			if h == name {
				for m.harnessCursor < i {
					m = stepThrough(t, m, downMsg())
				}
				for m.harnessCursor > i {
					m = stepThrough(t, m, tea.KeyMsg{Type: tea.KeyUp})
				}
				m = stepThrough(t, m, spaceMsg())
				want = append(want, name)
				break
			}
		}
	}
	m = stepThrough(t, m, enterMsg())
	if !m.done {
		t.Fatalf("model should be done after harness enter")
	}
	if !m.inputs.HarnessesSet {
		t.Fatalf("HarnessesSet must flip after enter")
	}
	if strings.Join(m.inputs.Harnesses, ",") != strings.Join(want, ",") {
		t.Fatalf("Harnesses: got %v want %v", m.inputs.Harnesses, want)
	}
}

// TestSetupPicker_CancelMidFlow asserts ctrl+c at any step aborts the
// model with no side-effect-bound inputs.
func TestSetupPicker_CancelMidFlow(t *testing.T) {
	needs := pickerNeeds{CLILang: true, TUILang: true, Agent: true, Preset: true, Harness: true}
	m, err := newSetupPickerModel(setupInputs{}, needs)
	if err != nil {
		t.Fatalf("newSetupPickerModel: %v", err)
	}
	m = stepThrough(t, m, downMsg(), enterMsg()) // pick pt-br
	if m.step != stepTUILang {
		t.Fatalf("setup precondition failed: step %v", m.step)
	}
	next, cmd := m.Update(ctrlCMsg())
	m = next.(setupPickerModel)
	if !m.aborted {
		t.Fatalf("ctrl+c must flip aborted")
	}
	if m.done {
		t.Fatalf("ctrl+c must not flip done")
	}
	if cmd == nil {
		t.Fatalf("ctrl+c must return tea.Quit cmd")
	}
}

// TestSetupPicker_SkipsResolvedSteps proves that env-supplied inputs
// collapse their pickers — supply CLI/TUI/agent up front and the model
// starts at stepPreset directly, mirroring AC §8's per-screen skip.
func TestSetupPicker_SkipsResolvedSteps(t *testing.T) {
	needs := pickerNeeds{Preset: true, Harness: true}
	m, err := newSetupPickerModel(setupInputs{
		CLILang:      "en",
		TUILang:      "en",
		AgentLang:    "English",
		AgentLangSet: true,
	}, needs)
	if err != nil {
		t.Fatalf("newSetupPickerModel: %v", err)
	}
	if m.step != stepPreset {
		t.Fatalf("step: got %v want stepPreset (only preset+harness remaining)", m.step)
	}
	if m.cliCatalog == nil || m.cliCatalog.Code != "en" {
		t.Fatalf("cliCatalog must be preloaded when CLILang is set up front")
	}
	if len(m.presets) == 0 {
		t.Fatalf("presets must be preloaded when starting at stepPreset")
	}
}

// TestSetupPicker_AllResolved exits immediately when nothing is needed.
func TestSetupPicker_AllResolved(t *testing.T) {
	m, err := newSetupPickerModel(setupInputs{
		CLILang:      "en",
		TUILang:      "en",
		AgentLang:    "English",
		AgentLangSet: true,
		Preset:       "omakase",
		Harnesses:    []string{"claude-code"},
		HarnessesSet: true,
	}, pickerNeeds{})
	if err != nil {
		t.Fatalf("newSetupPickerModel: %v", err)
	}
	if !m.done {
		t.Fatalf("model with no needs must land on done")
	}
}

// TestSetupPicker_HeadlessNoTTY exercises runSetupPicker's no-TTY guard:
// when stdin is not a terminal and at least one input is missing, it
// must return a validation error pointing the caller at the env-var
// surface rather than blocking on a tea.Program that has no input to
// read.
func TestSetupPicker_HeadlessNoTTY(t *testing.T) {
	if stdinIsTTY() {
		t.Skip("stdin is a TTY; this test exercises the headless branch")
	}
	needs := pickerNeeds{CLILang: true, TUILang: true, Agent: true, Preset: true, Harness: true}
	_, err := runSetupPicker(context.Background(), setupInputs{}, needs)
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "TTY") && !strings.Contains(err.Error(), "tty") {
		t.Fatalf("expected TTY-related message, got %q", err.Error())
	}
}

// TestSetupPicker_PresetTitlesLocalized asserts the preset rows use the
// chosen CLI catalog (so a pt-br pick renders pt-br titles), guarding
// AC §3.4 — preset titles + descriptions resolved through the catalog
// the user just picked.
func TestSetupPicker_PresetTitlesLocalized(t *testing.T) {
	m, err := newSetupPickerModel(setupInputs{CLILang: "pt-br"}, pickerNeeds{TUILang: true, Agent: true, Preset: true, Harness: true})
	if err != nil {
		t.Fatalf("newSetupPickerModel: %v", err)
	}
	if m.cliCatalog == nil {
		t.Fatalf("cliCatalog should be preloaded for pt-br")
	}
	// Force a transition through agent step so presets get populated.
	m.preparePresets()
	var omakase presetOption
	for _, p := range m.presets {
		if p.Name == "omakase" {
			omakase = p
			break
		}
	}
	if omakase.Name == "" {
		t.Fatalf("omakase preset not in resolved list: %+v", m.presets)
	}
	want := m.cliCatalog.Keys["cli.preset.omakase.title"]
	if want == "" {
		t.Fatalf("pt-br catalog missing cli.preset.omakase.title")
	}
	if omakase.Title != want {
		t.Fatalf("omakase title: got %q want %q (pt-br catalog)", omakase.Title, want)
	}
}
