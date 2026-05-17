package cli

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"omakiten/defaults"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/installer"
)

// setupStep identifies which screen the picker is currently rendering.
// Steps are advanced one at a time so a partially-supplied set of env
// vars (e.g. CLI lang set, TUI lang missing) lets the picker skip past
// the screens whose value is already known. stepDone is the terminal
// state that triggers tea.Quit.
type setupStep int

const (
	stepCLILang setupStep = iota
	stepTUILang
	stepAgentLang
	stepPreset
	stepHarness
	stepDone
)

// pickerNeeds tracks which inputs are still missing when the picker
// starts — values supplied via env or flag flip the corresponding bool
// to false so the step transition skips that screen entirely. Mirrors
// AC §8: each OKT_* env var collapses its own picker screen.
type pickerNeeds struct {
	CLILang bool
	TUILang bool
	Agent   bool
	Preset  bool
	Harness bool
}

// langOption is one row in the CLI/TUI language pickers. Code is the
// bundled language slug (en, pt-br); Native is the in-language label
// rendered alongside the code.
type langOption struct {
	Code   string
	Native string
}

// presetOption is one row in the preset picker. Title/Description are
// resolved against the chosen CLI language's catalog so the rows render
// in the user's freshly-picked language; Name is the canonical slug
// used by SeedInstall.
type presetOption struct {
	Name        string
	Title       string
	Description string
}

// setupPickerModel is the bubbletea program backing the interactive
// `okt setup`. It is decoupled from the cobra command so unit tests can
// feed tea.KeyMsg values into Update without spinning up a tea.Program
// — the round-trip teatest pattern would need a PTY this codebase does
// not have a dependency for, so we test the model functionally
// (keystroke in → state out) and reserve the program launch for the
// production code path.
type setupPickerModel struct {
	step  setupStep
	needs pickerNeeds

	inputs setupInputs

	langs       []langOption
	langCursor  int
	tuiCursor   int
	agentInput  textinput.Model
	presets     []presetOption
	presetCursor int
	harnesses    []string
	harnessCursor int
	harnessChosen map[int]bool

	// cliCatalog carries the bundled Language pack matching inputs.CLILang
	// — resolved as soon as step 0 confirms (or skipped via env var). The
	// keys map drives the titles + hints for screens 2-5. nil before
	// resolution; the View falls back to English via the package catalog
	// when the lookup misses.
	cliCatalog *config.Language

	aborted bool
	done    bool
}

// newSetupPickerModel constructs the bubbletea model with the rows
// needed for every screen pre-loaded so Update can route a key to the
// right slice without re-reading the embed FS mid-frame. Callers pass
// the partially-resolved inputs + a needs mask so already-supplied
// values short-circuit the corresponding screens.
//
// If inputs.CLILang is already non-empty (env var path), the chosen
// language's catalog is loaded eagerly so screens 2-5 render localized
// labels from the very first frame.
func newSetupPickerModel(inputs setupInputs, needs pickerNeeds) (setupPickerModel, error) {
	langs, err := loadBundledLanguageOptions()
	if err != nil {
		return setupPickerModel{}, err
	}
	if len(langs) == 0 {
		return setupPickerModel{}, domain.NewError(domain.ErrConfigInvalid, "no bundled languages available", nil)
	}

	model := setupPickerModel{
		step:          stepCLILang,
		needs:         needs,
		inputs:        inputs,
		langs:         langs,
		harnesses:     installer.SupportedHarnesses(),
		harnessChosen: map[int]bool{},
	}

	model.langCursor = indexOfLangCode(langs, inputs.CLILang)
	model.tuiCursor = indexOfLangCode(langs, firstNonEmpty(inputs.TUILang, inputs.CLILang))

	model.agentInput = textinput.New()
	model.agentInput.Prompt = "› "
	model.agentInput.CharLimit = 64
	model.agentInput.Width = 32

	// If the CLI lang is already known we can preload the catalog now so
	// the preset picker renders titles/descriptions in the chosen lang
	// the first time the user lands there.
	if inputs.CLILang != "" {
		if lang, err := config.LoadBundledLanguage(inputs.CLILang); err == nil {
			model.cliCatalog = &lang
		}
	}

	// Pre-fill harness selections from inputs so re-runs of --update can
	// show the user their existing choices with the cursor on the first
	// already-checked row.
	for i, name := range model.harnesses {
		for _, selected := range inputs.Harnesses {
			if selected == name {
				model.harnessChosen[i] = true
			}
		}
	}

	model.advancePastResolved()
	return model, nil
}

// Init satisfies tea.Model. We do not start the textinput cursor blink
// here because Init is called once at program start, before the user
// has navigated to the agent-lang screen; Update fires Cursor.BlinkCmd
// on the actual transition into stepAgentLang so we do not waste a
// frame ticking a hidden cursor on screens 1, 4, 5.
func (m setupPickerModel) Init() tea.Cmd { return nil }

// Update routes the incoming message to the active step's handler and
// returns the next model + cmd. Ctrl+C wins everywhere so users can
// always bail out before any side-effect.
func (m setupPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.Type {
		case tea.KeyCtrlC:
			m.aborted = true
			return m, tea.Quit
		}
	}

	switch m.step {
	case stepCLILang:
		return m.updateCLILang(msg)
	case stepTUILang:
		return m.updateTUILang(msg)
	case stepAgentLang:
		return m.updateAgentLang(msg)
	case stepPreset:
		return m.updatePreset(msg)
	case stepHarness:
		return m.updateHarness(msg)
	}
	return m, nil
}

func (m setupPickerModel) updateCLILang(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	rows := len(m.langs)
	switch key.String() {
	case "up", "k":
		if m.langCursor > 0 {
			m.langCursor--
		}
	case "down", "j":
		if m.langCursor < rows-1 {
			m.langCursor++
		}
	case "home", "g":
		m.langCursor = 0
	case "end", "G":
		m.langCursor = rows - 1
	case "enter":
		chosen := m.langs[m.langCursor].Code
		m.inputs.CLILang = chosen
		if lang, err := config.LoadBundledLanguage(chosen); err == nil {
			m.cliCatalog = &lang
		}
		// If the TUI cursor still sits on the original default, bring it
		// forward to the CLI choice so the next picker pre-selects the
		// matching row (matches AC §3.2: "TUI default = CLI choice").
		if !m.needs.CLILang {
			// shouldn't reach here, but guard anyway
		}
		m.tuiCursor = indexOfLangCode(m.langs, firstNonEmpty(m.inputs.TUILang, chosen))
		return m.transition(stepTUILang), nil
	}
	return m, nil
}

func (m setupPickerModel) updateTUILang(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	rows := len(m.langs)
	switch key.String() {
	case "up", "k":
		if m.tuiCursor > 0 {
			m.tuiCursor--
		}
	case "down", "j":
		if m.tuiCursor < rows-1 {
			m.tuiCursor++
		}
	case "home", "g":
		m.tuiCursor = 0
	case "end", "G":
		m.tuiCursor = rows - 1
	case "enter":
		m.inputs.TUILang = m.langs[m.tuiCursor].Code
		return m.transition(stepAgentLang), nil
	}
	return m, nil
}

func (m setupPickerModel) updateAgentLang(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.Type {
		case tea.KeyEnter:
			m.inputs.AgentLang = strings.TrimSpace(m.agentInput.Value())
			m.inputs.AgentLangSet = true
			m.preparePresets()
			return m.transition(stepPreset), nil
		}
	}
	var cmd tea.Cmd
	m.agentInput, cmd = m.agentInput.Update(msg)
	return m, cmd
}

func (m setupPickerModel) updatePreset(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	rows := len(m.presets)
	switch key.String() {
	case "up", "k":
		if m.presetCursor > 0 {
			m.presetCursor--
		}
	case "down", "j":
		if m.presetCursor < rows-1 {
			m.presetCursor++
		}
	case "home", "g":
		m.presetCursor = 0
	case "end", "G":
		m.presetCursor = rows - 1
	case "enter":
		m.inputs.Preset = m.presets[m.presetCursor].Name
		return m.transition(stepHarness), nil
	}
	return m, nil
}

func (m setupPickerModel) updateHarness(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	rows := len(m.harnesses)
	switch key.String() {
	case "up", "k":
		if m.harnessCursor > 0 {
			m.harnessCursor--
		}
	case "down", "j":
		if m.harnessCursor < rows-1 {
			m.harnessCursor++
		}
	case "home", "g":
		m.harnessCursor = 0
	case "end", "G":
		m.harnessCursor = rows - 1
	case " ", "space":
		m.harnessChosen[m.harnessCursor] = !m.harnessChosen[m.harnessCursor]
	case "enter":
		var chosen []string
		for i, name := range m.harnesses {
			if m.harnessChosen[i] {
				chosen = append(chosen, name)
			}
		}
		m.inputs.Harnesses = chosen
		m.inputs.HarnessesSet = true
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

// transition moves to next; skips screens whose value was already
// supplied via env var / flag (needs.* == false) so headless overrides
// collapse the corresponding pickers without flicker.
func (m setupPickerModel) transition(next setupStep) setupPickerModel {
	m.step = next
	m.advancePastResolved()
	return m
}

// advancePastResolved walks forward through any step whose value is
// already known. If every remaining step is resolved we land on
// stepDone and the surrounding tea.Program loop tears down. The agent
// textinput cursor starts blinking on entry into stepAgentLang via a
// Focus() so the cursor is visible the first frame.
func (m *setupPickerModel) advancePastResolved() {
	for {
		switch m.step {
		case stepCLILang:
			if m.needs.CLILang {
				return
			}
			m.step = stepTUILang
		case stepTUILang:
			if m.needs.TUILang {
				return
			}
			m.step = stepAgentLang
		case stepAgentLang:
			if m.needs.Agent {
				m.agentInput.Focus()
				return
			}
			m.preparePresets()
			m.step = stepPreset
		case stepPreset:
			if m.needs.Preset {
				return
			}
			m.step = stepHarness
		case stepHarness:
			if m.needs.Harness {
				return
			}
			m.done = true
			return
		default:
			return
		}
	}
}

// preparePresets resolves the four bundled presets' title + description
// against the chosen CLI catalog. Falls back to the bundled en pack on a
// missing key so a partial / custom language pack still renders
// reasonable rows instead of bare slugs.
func (m *setupPickerModel) preparePresets() {
	if len(m.presets) > 0 {
		return
	}
	names := installer.SupportedPresets()
	out := make([]presetOption, 0, len(names))
	for _, n := range names {
		titleKey := fmt.Sprintf("cli.preset.%s.title", n)
		descKey := fmt.Sprintf("cli.preset.%s.description", n)
		out = append(out, presetOption{
			Name:        n,
			Title:       m.translate(titleKey),
			Description: m.translate(descKey),
		})
	}
	m.presets = out
	if idx := indexOfPreset(out, m.inputs.Preset); idx >= 0 {
		m.presetCursor = idx
	}
}

// translate resolves key against the chosen CLI catalog, falling back
// to the package-level catalog (English by default) and finally to the
// bare key. Used for screen titles / hints after step 0 so the labels
// match the user's freshly-picked language.
func (m setupPickerModel) translate(key string) string {
	if m.cliCatalog != nil {
		if v, ok := m.cliCatalog.Keys[key]; ok && v != "" {
			return v
		}
	}
	return t(key)
}

// View renders the active screen. Layout is intentionally low-frills —
// the picker runs inside the install pipe (`curl … | bash`) where the
// host terminal may not support animations cleanly, so we stay with
// vanilla ANSI: title, blank line, rows, blank line, hint.
func (m setupPickerModel) View() string {
	switch m.step {
	case stepCLILang:
		return renderListView("Select your CLI language", "↑/↓ navigate · enter confirm · ctrl+c quit", langRows(m.langs, m.langCursor))
	case stepTUILang:
		return renderListView(m.translate("cli.setup.picker.lang.tui.title"), m.translate("cli.setup.picker.hint.nav"), langRows(m.langs, m.tuiCursor))
	case stepAgentLang:
		m.agentInput.Placeholder = m.translate("cli.setup.picker.agent.placeholder")
		return renderInputView(m.translate("cli.setup.picker.agent.title"), m.translate("cli.setup.picker.hint.input"), m.agentInput.View())
	case stepPreset:
		return renderListView(m.translate("cli.setup.picker.preset.title"), m.translate("cli.setup.picker.hint.nav"), presetRows(m.presets, m.presetCursor))
	case stepHarness:
		return renderListView(m.translate("cli.setup.picker.harness.title"), m.translate("cli.setup.picker.hint.multi"), harnessRows(m.harnesses, m.harnessChosen, m.harnessCursor))
	}
	return ""
}

func renderListView(title, hint string, rows []string) string {
	var b strings.Builder
	b.WriteString("\n// " + title + "\n\n")
	for _, row := range rows {
		b.WriteString(row + "\n")
	}
	b.WriteString("\n" + hint + "\n")
	return b.String()
}

func renderInputView(title, hint, field string) string {
	return "\n// " + title + "\n\n  " + field + "\n\n" + hint + "\n"
}

func langRows(langs []langOption, cursor int) []string {
	out := make([]string, 0, len(langs))
	for i, l := range langs {
		marker := "  "
		if i == cursor {
			marker = "› "
		}
		out = append(out, fmt.Sprintf("%s%s (%s)", marker, l.Native, l.Code))
	}
	return out
}

func presetRows(presets []presetOption, cursor int) []string {
	out := make([]string, 0, len(presets))
	for i, p := range presets {
		marker := "  "
		if i == cursor {
			marker = "› "
		}
		out = append(out, fmt.Sprintf("%s%-10s  %s", marker, p.Name, p.Title))
		out = append(out, "    "+p.Description)
	}
	return out
}

func harnessRows(harnesses []string, chosen map[int]bool, cursor int) []string {
	out := make([]string, 0, len(harnesses))
	for i, name := range harnesses {
		marker := "  "
		if i == cursor {
			marker = "› "
		}
		box := "[ ]"
		if chosen[i] {
			box = "[x]"
		}
		out = append(out, fmt.Sprintf("%s%s %s", marker, box, name))
	}
	return out
}

func indexOfLangCode(langs []langOption, code string) int {
	if code == "" {
		return 0
	}
	for i, l := range langs {
		if l.Code == code {
			return i
		}
	}
	return 0
}

func indexOfPreset(presets []presetOption, name string) int {
	if name == "" {
		return -1
	}
	for i, p := range presets {
		if p.Name == name {
			return i
		}
	}
	return -1
}

// loadBundledLanguageOptions enumerates every `<code>.yaml` shipped under
// defaults/languages and returns one langOption per pack, sorted by code
// for deterministic ordering. The picker uses this rather than
// config.LoadLanguages because the latter wants an on-disk install root
// that does not yet exist on a fresh `curl|bash` run.
func loadBundledLanguageOptions() ([]langOption, error) {
	entries, err := defaults.FS.ReadDir("languages")
	if err != nil {
		return nil, fmt.Errorf("read bundled languages: %w", err)
	}
	var codes []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".yaml") {
			continue
		}
		codes = append(codes, strings.TrimSuffix(name, ".yaml"))
	}
	sort.Strings(codes)
	out := make([]langOption, 0, len(codes))
	for _, code := range codes {
		lang, err := config.LoadBundledLanguage(code)
		if err != nil {
			return nil, err
		}
		native := lang.Native
		if native == "" {
			native = lang.Name
		}
		if native == "" {
			native = code
		}
		out = append(out, langOption{Code: code, Native: native})
	}
	return out, nil
}

// runSetupPicker launches the bubbletea program when at least one
// picker screen needs the user's input. Returns the populated inputs on
// success, a coded ErrCanceled-style error on ctrl+c, or a
// validation_error when stdin is not a TTY (the caller is meant to fall
// back to the headless contract in that case — env vars / flags must
// cover every needed input).
func runSetupPicker(ctx context.Context, inputs setupInputs, needs pickerNeeds) (setupInputs, error) {
	if !pickerNeeded(needs) {
		return inputs, nil
	}
	if !stdinIsTTY() {
		return setupInputs{}, domain.NewError(domain.ErrValidation, t("cli.setup.picker.no_tty"), map[string]any{
			"cli_lang_needed": needs.CLILang,
			"tui_lang_needed": needs.TUILang,
			"agent_needed":    needs.Agent,
			"preset_needed":   needs.Preset,
			"harness_needed":  needs.Harness,
		})
	}
	model, err := newSetupPickerModel(inputs, needs)
	if err != nil {
		return setupInputs{}, err
	}
	prog := tea.NewProgram(model, tea.WithContext(ctx), tea.WithInput(os.Stdin), tea.WithOutput(os.Stderr))
	final, err := prog.Run()
	if err != nil {
		return setupInputs{}, fmt.Errorf("run setup picker: %w", err)
	}
	result := final.(setupPickerModel)
	if result.aborted {
		return setupInputs{}, domain.NewError(domain.ErrValidation, t("cli.setup.picker.aborted"), nil)
	}
	if !result.done {
		return setupInputs{}, domain.NewError(domain.ErrValidation, t("cli.setup.picker.aborted"), nil)
	}
	return result.inputs, nil
}

func pickerNeeded(n pickerNeeds) bool {
	return n.CLILang || n.TUILang || n.Agent || n.Preset || n.Harness
}

func stdinIsTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}
