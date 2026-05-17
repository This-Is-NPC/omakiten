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
