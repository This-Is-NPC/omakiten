package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/installer"
)

func keyMsg(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func enterMsg() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEnter} }
func ctrlCMsg() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyCtrlC} }
func downMsg() tea.KeyMsg  { return tea.KeyMsg{Type: tea.KeyDown} }
func tabMsg() tea.KeyMsg   { return tea.KeyMsg{Type: tea.KeyTab} }
func escMsg() tea.KeyMsg   { return tea.KeyMsg{Type: tea.KeyEsc} }
func pgupMsg() tea.KeyMsg  { return tea.KeyMsg{Type: tea.KeyPgUp} }

func stepThrough(t *testing.T, m setupPickerModel, msgs ...tea.Msg) setupPickerModel {
	t.Helper()
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		m = next.(setupPickerModel)
	}
	return m
}

// TestSetupPicker_HappyPath drives a fresh install through every
// screen (lang=pt-br → agent="Portugues" → preset=omakase →
// harnesses=claude-code+opencode), then asserts the resulting inputs
// match what runSetup would receive. Language is one screen now — CLI
// and TUI share the same picker per UX feedback.
func TestSetupPicker_HappyPath(t *testing.T) {
	needs := pickerNeeds{Lang: true, Agent: true, Preset: true, Harness: true}
	m, err := newSetupPickerModel(setupInputs{}, needs)
	if err != nil {
		t.Fatalf("newSetupPickerModel: %v", err)
	}
	if m.step != stepLang {
		t.Fatalf("step: got %v want stepLang", m.step)
	}

	// Lang — bundled order is alphabetical by code; find pt-br
	// dynamically so adding a new bundled language pack does not break
	// the test by shifting cursor indices.
	ptBRIdx := -1
	for i, l := range m.langs {
		if l.Code == "pt-br" {
			ptBRIdx = i
			break
		}
	}
	if ptBRIdx < 0 {
		t.Fatalf("pt-br not in bundled language options: %+v", m.langs)
	}
	for i := 0; i < ptBRIdx; i++ {
		m = stepThrough(t, m, downMsg())
	}
	m = stepThrough(t, m, enterMsg())
	if m.step != stepAgentLang {
		t.Fatalf("after lang confirm: step %v want stepAgentLang", m.step)
	}
	if m.inputs.CLILang != "pt-br" || m.inputs.TUILang != "pt-br" {
		t.Fatalf("languages: got cli=%q tui=%q want both pt-br", m.inputs.CLILang, m.inputs.TUILang)
	}
	if m.cliCatalog == nil || m.cliCatalog.Code != "pt-br" {
		t.Fatalf("cliCatalog not loaded for pt-br: %+v", m.cliCatalog)
	}

	// Agent lang — type "Portugues" and enter.
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

	// Preset — move cursor to omakase then enter.
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

	// Harness — toggle claude-code and opencode with enter, then submit
	// with tab. enter toggles a row in/out of the selection; tab
	// finalises and quits the program.
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
				m = stepThrough(t, m, enterMsg())
				want = append(want, name)
				break
			}
		}
	}
	m = stepThrough(t, m, tabMsg())
	if !m.done {
		t.Fatalf("model should be done after tab confirm")
	}
	if !m.inputs.HarnessesSet {
		t.Fatalf("HarnessesSet must flip after tab")
	}
	if strings.Join(m.inputs.Harnesses, ",") != strings.Join(want, ",") {
		t.Fatalf("Harnesses: got %v want %v", m.inputs.Harnesses, want)
	}
}

func TestSetupPicker_CancelMidFlow(t *testing.T) {
	needs := pickerNeeds{Lang: true, Agent: true, Preset: true, Harness: true}
	m, err := newSetupPickerModel(setupInputs{}, needs)
	if err != nil {
		t.Fatalf("newSetupPickerModel: %v", err)
	}
	m = stepThrough(t, m, downMsg(), enterMsg()) // pick pt-br
	if m.step != stepAgentLang {
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

// TestSetupPicker_SkipsResolvedSteps proves env-supplied inputs collapse
// their pickers — supply lang/agent up front and the model starts at
// stepPreset directly.
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

func TestSetupPicker_HeadlessNoTTY(t *testing.T) {
	if stdinIsTTY() {
		t.Skip("stdin is a TTY; this test exercises the headless branch")
	}
	needs := pickerNeeds{Lang: true, Agent: true, Preset: true, Harness: true}
	_, err := runSetupPicker(context.Background(), setupInputs{}, needs)
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "TTY") && !strings.Contains(err.Error(), "tty") {
		t.Fatalf("expected TTY-related message, got %q", err.Error())
	}
}

// TestSetupPicker_IncludesCustomLanguages asserts a user-authored
// language pack at <ConfigRoot>/languages/custom/<code>.yaml shows up
// in the picker alongside the bundled defaults. This is the
// `okt setup --update` story: the install picker re-reads custom packs
// each invocation so a new language pack the user dropped in since the
// last setup becomes selectable without rebuilding the binary.
func TestSetupPicker_IncludesCustomLanguages(t *testing.T) {
	root := t.TempDir()
	customDir := filepath.Join(root, "languages", "custom")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom: %v", err)
	}
	yamlBody := "code: xx\nname: TestLang\nnative: TestNative\nkeys: {}\n"
	if err := os.WriteFile(filepath.Join(customDir, "xx.yaml"), []byte(yamlBody), 0o644); err != nil {
		t.Fatalf("write custom yaml: %v", err)
	}
	t.Setenv("OMAKITEN_HOME", root)
	t.Setenv("XDG_CONFIG_HOME", "")

	needs := pickerNeeds{Lang: true, Agent: true, Preset: true, Harness: true}
	m, err := newSetupPickerModel(setupInputs{}, needs)
	if err != nil {
		t.Fatalf("newSetupPickerModel: %v", err)
	}
	found := false
	for _, l := range m.langs {
		if l.Code == "xx" && l.Native == "TestNative" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("custom language 'xx' not in picker rows: %+v", m.langs)
	}
}

// TestSetupPicker_CustomOverridesBundled — same code in custom replaces
// the bundled Native label.
func TestSetupPicker_CustomOverridesBundled(t *testing.T) {
	root := t.TempDir()
	customDir := filepath.Join(root, "languages", "custom")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom: %v", err)
	}
	yamlBody := "code: en\nname: English-Override\nnative: OverrideNative\nkeys: {}\n"
	if err := os.WriteFile(filepath.Join(customDir, "en.yaml"), []byte(yamlBody), 0o644); err != nil {
		t.Fatalf("write custom yaml: %v", err)
	}
	t.Setenv("OMAKITEN_HOME", root)
	t.Setenv("XDG_CONFIG_HOME", "")

	needs := pickerNeeds{Lang: true, Agent: true, Preset: true, Harness: true}
	m, err := newSetupPickerModel(setupInputs{}, needs)
	if err != nil {
		t.Fatalf("newSetupPickerModel: %v", err)
	}
	for _, l := range m.langs {
		if l.Code == "en" {
			if l.Native != "OverrideNative" {
				t.Fatalf("en native: got %q want OverrideNative (custom must override bundled)", l.Native)
			}
			return
		}
	}
	t.Fatalf("en code not present in picker")
}

// TestSetupPicker_PrevFromEachStep — esc on any non-first active step
// returns the picker to the previous active step. Covers AC §6(a).
func TestSetupPicker_PrevFromEachStep(t *testing.T) {
	needs := pickerNeeds{Lang: true, Agent: true, Preset: true, Harness: true}
	m, err := newSetupPickerModel(setupInputs{}, needs)
	if err != nil {
		t.Fatalf("newSetupPickerModel: %v", err)
	}

	// stepLang → stepAgentLang, then esc → stepLang
	m = stepThrough(t, m, enterMsg())
	if m.step != stepAgentLang {
		t.Fatalf("want stepAgentLang got %v", m.step)
	}
	m = stepThrough(t, m, escMsg())
	if m.step != stepLang {
		t.Fatalf("esc from agent: want stepLang got %v", m.step)
	}

	// stepLang → agent → preset, then esc → agent
	m = stepThrough(t, m, enterMsg(), enterMsg())
	if m.step != stepPreset {
		t.Fatalf("want stepPreset got %v", m.step)
	}
	m = stepThrough(t, m, escMsg())
	if m.step != stepAgentLang {
		t.Fatalf("esc from preset: want stepAgentLang got %v", m.step)
	}
	if !m.agentInput.Focused() {
		t.Fatalf("agentInput must re-focus after prev to stepAgentLang")
	}

	// agent → preset → harness, then esc → preset
	m = stepThrough(t, m, enterMsg(), enterMsg())
	if m.step != stepHarness {
		t.Fatalf("want stepHarness got %v", m.step)
	}
	m = stepThrough(t, m, escMsg())
	if m.step != stepPreset {
		t.Fatalf("esc from harness: want stepPreset got %v", m.step)
	}
}

// TestSetupPicker_PrevSkipsEnvCollapsed — when agent step is supplied
// up front, esc from preset must skip past it back to stepLang.
// Covers AC §6(b).
func TestSetupPicker_PrevSkipsEnvCollapsed(t *testing.T) {
	needs := pickerNeeds{Lang: true, Preset: true, Harness: true}
	m, err := newSetupPickerModel(setupInputs{
		AgentLang:    "English",
		AgentLangSet: true,
	}, needs)
	if err != nil {
		t.Fatalf("newSetupPickerModel: %v", err)
	}
	if m.step != stepLang {
		t.Fatalf("want stepLang got %v", m.step)
	}
	m = stepThrough(t, m, enterMsg())
	if m.step != stepPreset {
		t.Fatalf("post-lang must skip collapsed agent: want stepPreset got %v", m.step)
	}
	m = stepThrough(t, m, escMsg())
	if m.step != stepLang {
		t.Fatalf("prev must skip collapsed agent: want stepLang got %v", m.step)
	}
}

// TestSetupPicker_ValuesPreservedAcrossPrev — choices entered before
// the user backs up survive the round trip. Covers AC §6(c).
func TestSetupPicker_ValuesPreservedAcrossPrev(t *testing.T) {
	needs := pickerNeeds{Lang: true, Agent: true, Preset: true, Harness: true}
	m, err := newSetupPickerModel(setupInputs{}, needs)
	if err != nil {
		t.Fatalf("newSetupPickerModel: %v", err)
	}
	// Move lang cursor off zero, confirm.
	m = stepThrough(t, m, downMsg(), enterMsg())
	wantLangCursor := m.langCursor
	if wantLangCursor == 0 {
		t.Fatalf("test setup: bundled langs need at least 2 entries")
	}
	// Type agent name, confirm to land on preset.
	m = stepThrough(t, m, keyMsg('P'), keyMsg('t'), enterMsg())
	if m.step != stepPreset {
		t.Fatalf("want stepPreset got %v", m.step)
	}
	// Move preset cursor.
	if len(m.presets) > 1 {
		m = stepThrough(t, m, downMsg())
	}
	wantPresetCursor := m.presetCursor

	// esc all the way back to lang, then walk forward.
	m = stepThrough(t, m, escMsg())
	if m.step != stepAgentLang {
		t.Fatalf("want stepAgentLang got %v", m.step)
	}
	if m.agentInput.Value() != "Pt" {
		t.Fatalf("agentInput value lost: %q", m.agentInput.Value())
	}
	if !m.agentInput.Focused() {
		t.Fatalf("agentInput must re-focus on prev landing")
	}
	m = stepThrough(t, m, escMsg())
	if m.step != stepLang {
		t.Fatalf("want stepLang got %v", m.step)
	}
	if m.langCursor != wantLangCursor {
		t.Fatalf("langCursor lost: got %d want %d", m.langCursor, wantLangCursor)
	}

	// Forward through the chain — agent text and preset cursor must
	// survive the round-trip.
	m = stepThrough(t, m, enterMsg())
	if m.agentInput.Value() != "Pt" {
		t.Fatalf("agentInput value lost on re-entry: %q", m.agentInput.Value())
	}
	m = stepThrough(t, m, enterMsg())
	if m.step != stepPreset {
		t.Fatalf("want stepPreset got %v", m.step)
	}
	if m.presetCursor != wantPresetCursor {
		t.Fatalf("presetCursor lost: got %d want %d", m.presetCursor, wantPresetCursor)
	}

	// Move forward to harness, toggle one, esc back, return, confirm
	// harnessChosen survived.
	m = stepThrough(t, m, enterMsg())
	if m.step != stepHarness {
		t.Fatalf("want stepHarness got %v", m.step)
	}
	m = stepThrough(t, m, enterMsg()) // toggle row 0
	if !m.harnessChosen[0] {
		t.Fatalf("harnessChosen[0] should be true after enter")
	}
	wantCursor := m.harnessCursor
	m = stepThrough(t, m, escMsg())
	if m.step != stepPreset {
		t.Fatalf("esc from harness: want stepPreset got %v", m.step)
	}
	m = stepThrough(t, m, enterMsg())
	if m.step != stepHarness {
		t.Fatalf("re-entry: want stepHarness got %v", m.step)
	}
	if !m.harnessChosen[0] {
		t.Fatalf("harnessChosen lost on round trip")
	}
	if m.harnessCursor != wantCursor {
		t.Fatalf("harnessCursor lost: got %d want %d", m.harnessCursor, wantCursor)
	}
}

// TestSetupPicker_PrevNoOpOnFirstStep — esc on the first active step
// must not quit or panic. Covers AC §6(d).
func TestSetupPicker_PrevNoOpOnFirstStep(t *testing.T) {
	needs := pickerNeeds{Lang: true, Agent: true, Preset: true, Harness: true}
	m, err := newSetupPickerModel(setupInputs{}, needs)
	if err != nil {
		t.Fatalf("newSetupPickerModel: %v", err)
	}
	next, cmd := m.Update(escMsg())
	m = next.(setupPickerModel)
	if cmd != nil {
		t.Fatalf("esc on first step must not return a cmd")
	}
	if m.aborted {
		t.Fatalf("esc on first step must not abort")
	}
	if m.done {
		t.Fatalf("esc on first step must not complete")
	}
	if m.step != stepLang {
		t.Fatalf("step must stay at stepLang, got %v", m.step)
	}

	// Same check on a needs={Preset:true,Harness:true} model — preset is
	// the first active step there, esc must be a no-op.
	m2, err := newSetupPickerModel(setupInputs{
		CLILang: "en", TUILang: "en", AgentLang: "English", AgentLangSet: true,
	}, pickerNeeds{Preset: true, Harness: true})
	if err != nil {
		t.Fatalf("newSetupPickerModel: %v", err)
	}
	next2, cmd2 := m2.Update(escMsg())
	m2 = next2.(setupPickerModel)
	if cmd2 != nil || m2.aborted || m2.step != stepPreset {
		t.Fatalf("esc on first active step (preset) must no-op: step=%v aborted=%v cmd!=nil=%v", m2.step, m2.aborted, cmd2 != nil)
	}
}

// TestSetupPicker_StepIndicatorCount — paginator TotalPages tracks the
// count of needs-active steps; advancing changes the rendered page.
// Covers AC §6(e).
func TestSetupPicker_StepIndicatorCount(t *testing.T) {
	cases := []struct {
		name  string
		needs pickerNeeds
		want  int
	}{
		{"all four", pickerNeeds{Lang: true, Agent: true, Preset: true, Harness: true}, 4},
		{"preset+harness", pickerNeeds{Preset: true, Harness: true}, 2},
		{"lang+harness", pickerNeeds{Lang: true, Harness: true}, 2},
		{"agent only", pickerNeeds{Agent: true}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := newSetupPickerModel(setupInputs{
				CLILang: "en", TUILang: "en", AgentLang: "English", AgentLangSet: true,
				Preset: "omakase",
			}, tc.needs)
			if err != nil {
				t.Fatalf("newSetupPickerModel: %v", err)
			}
			if m.pager.TotalPages != tc.want {
				t.Fatalf("TotalPages: got %d want %d", m.pager.TotalPages, tc.want)
			}
			if len(m.activeOrder) != tc.want {
				t.Fatalf("activeOrder len: got %d want %d", len(m.activeOrder), tc.want)
			}
		})
	}

	// Walk a full picker — page should track the active index after
	// every transition.
	m, err := newSetupPickerModel(setupInputs{}, pickerNeeds{Lang: true, Agent: true, Preset: true, Harness: true})
	if err != nil {
		t.Fatalf("newSetupPickerModel: %v", err)
	}
	if m.pager.Page != 0 {
		t.Fatalf("start page: got %d want 0", m.pager.Page)
	}
	m = stepThrough(t, m, enterMsg())
	if m.pager.Page != 1 {
		t.Fatalf("after lang confirm: got %d want 1", m.pager.Page)
	}
	m = stepThrough(t, m, enterMsg())
	if m.pager.Page != 2 {
		t.Fatalf("after agent confirm: got %d want 2", m.pager.Page)
	}
	m = stepThrough(t, m, escMsg())
	if m.pager.Page != 1 {
		t.Fatalf("after esc: got %d want 1", m.pager.Page)
	}
}

// TestSetupPicker_PrevKeyVariants — esc, pgup, left, h all trigger
// prev on list steps; only esc and pgup trigger on the agent textinput
// (left/h are reserved for text editing).
func TestSetupPicker_PrevKeyVariants(t *testing.T) {
	listKeys := []tea.KeyMsg{
		escMsg(),
		pgupMsg(),
		{Type: tea.KeyLeft},
		{Type: tea.KeyRunes, Runes: []rune{'h'}},
	}
	for _, k := range listKeys {
		t.Run("preset/"+k.String(), func(t *testing.T) {
			m, err := newSetupPickerModel(setupInputs{
				CLILang: "en", TUILang: "en", AgentLang: "English", AgentLangSet: true,
			}, pickerNeeds{Preset: true, Harness: true})
			if err != nil {
				t.Fatalf("newSetupPickerModel: %v", err)
			}
			m = stepThrough(t, m, enterMsg()) // advance to harness
			if m.step != stepHarness {
				t.Fatalf("setup precondition: want stepHarness got %v", m.step)
			}
			m = stepThrough(t, m, k)
			if m.step != stepPreset {
				t.Fatalf("key %q: want stepPreset got %v", k.String(), m.step)
			}
		})
	}

	// Agent step — pgup goes back, 'h' is text input.
	m, err := newSetupPickerModel(setupInputs{}, pickerNeeds{Lang: true, Agent: true, Preset: true, Harness: true})
	if err != nil {
		t.Fatalf("newSetupPickerModel: %v", err)
	}
	m = stepThrough(t, m, enterMsg()) // → stepAgentLang
	if m.step != stepAgentLang {
		t.Fatalf("want stepAgentLang got %v", m.step)
	}
	// Type 'h' — must stay on agent step and append to text.
	m = stepThrough(t, m, keyMsg('h'))
	if m.step != stepAgentLang {
		t.Fatalf("'h' must not back out of agent step: got %v", m.step)
	}
	if m.agentInput.Value() != "h" {
		t.Fatalf("'h' must reach textinput: got %q", m.agentInput.Value())
	}
	// pgup — must back out.
	m = stepThrough(t, m, pgupMsg())
	if m.step != stepLang {
		t.Fatalf("pgup on agent: want stepLang got %v", m.step)
	}
}

// TestSetupPicker_BackHintGating — backHint() returns "" on the first
// active step (esc is a no-op there, advertising it would teach a dead
// key) and the " · esc back" suffix on every later step.
func TestSetupPicker_BackHintGating(t *testing.T) {
	m, err := newSetupPickerModel(setupInputs{}, pickerNeeds{Lang: true, Agent: true, Preset: true, Harness: true})
	if err != nil {
		t.Fatalf("newSetupPickerModel: %v", err)
	}
	if got := m.backHint(); got != "" {
		t.Fatalf("first step backHint: got %q want empty", got)
	}
	m = stepThrough(t, m, enterMsg())
	if got := m.backHint(); got == "" {
		t.Fatalf("post-advance backHint: got empty want non-empty")
	}
	// Walk back to first step — hint must disappear again.
	m = stepThrough(t, m, escMsg())
	if m.step != stepLang {
		t.Fatalf("setup precondition: want stepLang got %v", m.step)
	}
	if got := m.backHint(); got != "" {
		t.Fatalf("after returning to first step backHint: got %q want empty", got)
	}
}

// TestSetupPicker_ViewRendersStepIndicator — the rendered View output
// must contain the literal "step N/M" produced by the paginator. Guards
// against a regression where ArabicFormat is dropped, the paginator
// View() output changes shape, or stepIndicator stops being wired into
// renderListView / renderInputView.
func TestSetupPicker_ViewRendersStepIndicator(t *testing.T) {
	m, err := newSetupPickerModel(setupInputs{}, pickerNeeds{Lang: true, Agent: true, Preset: true, Harness: true})
	if err != nil {
		t.Fatalf("newSetupPickerModel: %v", err)
	}
	if got := m.View(); !strings.Contains(got, "step 1/4") {
		t.Fatalf("lang view: missing 'step 1/4' indicator\n%s", got)
	}
	m = stepThrough(t, m, enterMsg()) // → agent (input view)
	if got := m.View(); !strings.Contains(got, "step 2/4") {
		t.Fatalf("agent view: missing 'step 2/4' indicator\n%s", got)
	}
	m = stepThrough(t, m, enterMsg()) // → preset
	if got := m.View(); !strings.Contains(got, "step 3/4") {
		t.Fatalf("preset view: missing 'step 3/4' indicator\n%s", got)
	}
	m = stepThrough(t, m, enterMsg()) // → harness
	if got := m.View(); !strings.Contains(got, "step 4/4") {
		t.Fatalf("harness view: missing 'step 4/4' indicator\n%s", got)
	}
}

// TestSetupPicker_StepIndicatorHiddenForSingleStep — when only one
// step is active a "step 1/1" indicator is visual noise; stepIndicator
// must return empty and the View must not contain "step".
func TestSetupPicker_StepIndicatorHiddenForSingleStep(t *testing.T) {
	m, err := newSetupPickerModel(setupInputs{
		CLILang: "en", TUILang: "en", AgentLang: "English", AgentLangSet: true,
		Preset: "omakase",
	}, pickerNeeds{Harness: true})
	if err != nil {
		t.Fatalf("newSetupPickerModel: %v", err)
	}
	if m.step != stepHarness {
		t.Fatalf("setup precondition: want stepHarness got %v", m.step)
	}
	if got := m.stepIndicator(); got != "" {
		t.Fatalf("single-step stepIndicator: got %q want empty", got)
	}
	if got := m.View(); strings.Contains(got, "step ") {
		t.Fatalf("single-step view must not render indicator:\n%s", got)
	}
}

// TestSetupPicker_PresetTitlesLocalized asserts the preset rows use the
// chosen CLI catalog (so a pt-br pick renders pt-br titles).
func TestSetupPicker_PresetTitlesLocalized(t *testing.T) {
	m, err := newSetupPickerModel(setupInputs{CLILang: "pt-br"}, pickerNeeds{Agent: true, Preset: true, Harness: true})
	if err != nil {
		t.Fatalf("newSetupPickerModel: %v", err)
	}
	if m.cliCatalog == nil {
		t.Fatalf("cliCatalog should be preloaded for pt-br")
	}
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
