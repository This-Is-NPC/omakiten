package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/paginator"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"

	"omakiten/defaults"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/installer"
	"omakiten/internal/paths"
)

// setupStep identifies which screen the picker is currently rendering.
// Steps are advanced one at a time so a partially-supplied set of env
// vars (e.g. preset set, harness missing) lets the picker skip past
// screens whose value is already known. stepDone is the terminal state
// that triggers tea.Quit.
//
// CLI + TUI language share a single picker (stepLang): the per-surface
// split is a configuration knob inside omakiten.yaml the user can flip
// later via `okt config language`; the install picker keeps the flow
// short by writing both fields to the same code.
type setupStep int

const (
	stepLang setupStep = iota
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
	Lang    bool
	Agent   bool
	Preset  bool
	Harness bool
}

// langOption is one row in the language picker. Code is the bundled
// language slug (en, pt-br); Native is the in-language label rendered
// alongside the code.
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

// setupStyles bundles the lipgloss styles the picker views render
// against. Built once from the bundled omakiten theme so the install
// surface matches the rest of the TUI rather than rendering as
// undecorated gray text.
type setupStyles struct {
	title    lipgloss.Style
	row      lipgloss.Style
	rowFocus lipgloss.Style
	marker   lipgloss.Style
	hint     lipgloss.Style
	desc     lipgloss.Style
	box      lipgloss.Style
	boxOn    lipgloss.Style
}

// setupPickerModel is the bubbletea program backing the interactive
// `okt setup`. It is decoupled from the cobra command so unit tests can
// feed tea.KeyMsg values into Update without spinning up a tea.Program.
type setupPickerModel struct {
	step  setupStep
	needs pickerNeeds

	inputs setupInputs

	langs         []langOption
	langCursor    int
	agentInput    textinput.Model
	presets       []presetOption
	presetCursor  int
	harnesses     []string
	harnessCursor int
	harnessChosen map[int]bool

	// cliCatalog carries the bundled Language pack matching the chosen
	// language — resolved as soon as the lang step confirms (or eagerly
	// when env vars pre-fill it). Drives titles + hints for screens 2-5.
	// nil before resolution; the View falls back to the package catalog
	// (English by default) when the lookup misses.
	cliCatalog *config.Language

	styles setupStyles

	// activeOrder is the ordered slice of needs-active steps. Drives the
	// step indicator count and the back-navigation chain: env-collapsed
	// steps are excluded so esc/pgup skips them rather than landing on a
	// pre-resolved screen.
	activeOrder []setupStep
	pager       paginator.Model

	aborted bool
	done    bool
}

// newSetupPickerModel constructs the bubbletea model with the rows
// needed for every screen pre-loaded so Update can route a key to the
// right slice without re-reading the embed FS mid-frame.
func newSetupPickerModel(inputs setupInputs, needs pickerNeeds) (setupPickerModel, error) {
	langs, err := loadBundledLanguageOptions()
	if err != nil {
		return setupPickerModel{}, err
	}
	if len(langs) == 0 {
		return setupPickerModel{}, domain.NewError(domain.ErrConfigInvalid, "no bundled languages available", nil)
	}

	theme, _ := loadBundledTheme()

	activeOrder := computeActiveOrder(needs)
	pager := paginator.New()
	pager.Type = paginator.Arabic
	pager.PerPage = 1
	pager.ArabicFormat = "step %d/%d"
	pager.TotalPages = len(activeOrder)
	if pager.TotalPages < 1 {
		pager.TotalPages = 1
	}

	model := setupPickerModel{
		step:          stepLang,
		needs:         needs,
		inputs:        inputs,
		langs:         langs,
		harnesses:     installer.SupportedHarnesses(),
		harnessChosen: map[int]bool{},
		styles:        newSetupStyles(theme),
		activeOrder:   activeOrder,
		pager:         pager,
	}

	model.langCursor = indexOfLangCode(langs, firstNonEmpty(inputs.CLILang, inputs.TUILang))

	model.agentInput = textinput.New()
	model.agentInput.Prompt = "› "
	model.agentInput.CharLimit = 64
	model.agentInput.Width = 32
	model.agentInput.PromptStyle = model.styles.marker
	model.agentInput.TextStyle = model.styles.rowFocus

	if inputs.CLILang != "" {
		if lang, err := config.LoadBundledLanguage(inputs.CLILang); err == nil {
			model.cliCatalog = &lang
		}
	}

	for i, name := range model.harnesses {
		for _, selected := range inputs.Harnesses {
			if selected == name {
				model.harnessChosen[i] = true
			}
		}
	}

	model.advancePastResolved()
	model.syncPager()
	return model, nil
}

// computeActiveOrder returns the ordered slice of steps the user must
// actually answer — env-supplied or flag-supplied inputs collapse their
// step out of the list. Drives both the step indicator denominator and
// the back-nav chain so prev hops skip resolved screens.
func computeActiveOrder(n pickerNeeds) []setupStep {
	out := make([]setupStep, 0, 4)
	if n.Lang {
		out = append(out, stepLang)
	}
	if n.Agent {
		out = append(out, stepAgentLang)
	}
	if n.Preset {
		out = append(out, stepPreset)
	}
	if n.Harness {
		out = append(out, stepHarness)
	}
	return out
}

// Init satisfies tea.Model.
func (m setupPickerModel) Init() tea.Cmd { return nil }

// Update routes the incoming message to the active step's handler.
// Ctrl+C wins everywhere so users can always bail out before any
// side-effect.
func (m setupPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && key.Type == tea.KeyCtrlC {
		m.aborted = true
		return m, tea.Quit
	}

	switch m.step {
	case stepLang:
		return m.updateLang(msg)
	case stepAgentLang:
		return m.updateAgentLang(msg)
	case stepPreset:
		return m.updatePreset(msg)
	case stepHarness:
		return m.updateHarness(msg)
	}
	return m, nil
}

func (m setupPickerModel) updateLang(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if isPrevListKey(key.String()) {
		return m.goPrev(), nil
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
		m.inputs.TUILang = chosen
		if lang, err := config.LoadBundledLanguage(chosen); err == nil {
			m.cliCatalog = &lang
		}
		return m.transition(stepAgentLang), nil
	}
	return m, nil
}

func (m setupPickerModel) updateAgentLang(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		// Only esc/pgup intercept on this screen — `left` and `h` are
		// legitimate text-editing input the user might type into the
		// agent-language name (e.g. "Hindi"), so passing them through to
		// the textinput keeps the field usable.
		if isPrevInputKey(key.String()) {
			return m.goPrev(), nil
		}
		if key.Type == tea.KeyEnter {
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
	if isPrevListKey(key.String()) {
		return m.goPrev(), nil
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
	if isPrevListKey(key.String()) {
		return m.goPrev(), nil
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
	case "enter":
		m.harnessChosen[m.harnessCursor] = !m.harnessChosen[m.harnessCursor]
	case "tab":
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

func (m setupPickerModel) transition(next setupStep) setupPickerModel {
	m.step = next
	m.advancePastResolved()
	m.syncPager()
	return m
}

// goPrev moves the picker back to the previous needs-active step.
// No-op when the current step is the first active one or is not part of
// the active chain (e.g. stepDone). Re-focuses the agent textinput when
// landing back on it so the cursor blinks and the field accepts keys.
func (m setupPickerModel) goPrev() setupPickerModel {
	idx := m.activeIndex()
	if idx <= 0 {
		return m
	}
	m.step = m.activeOrder[idx-1]
	if m.step == stepAgentLang {
		m.agentInput.Focus()
	}
	m.syncPager()
	return m
}

// activeIndex returns the position of the current step within
// activeOrder, or -1 if the step has been resolved out (terminal
// stepDone, or any step whose needs.* flag was false at construction).
func (m setupPickerModel) activeIndex() int {
	for i, s := range m.activeOrder {
		if s == m.step {
			return i
		}
	}
	return -1
}

// syncPager points the paginator at the current active index so the
// rendered "step N/M" matches the screen the user is looking at. Leaves
// the page alone when the step is not in activeOrder (e.g. stepDone) —
// the View short-circuits in those states so the stale page never
// renders.
func (m *setupPickerModel) syncPager() {
	if idx := m.activeIndex(); idx >= 0 {
		m.pager.Page = idx
	}
}

func isPrevListKey(s string) bool {
	switch s {
	case "esc", "left", "h", "pgup":
		return true
	}
	return false
}

func isPrevInputKey(s string) bool {
	switch s {
	case "esc", "pgup":
		return true
	}
	return false
}

// advancePastResolved walks forward through any step whose value is
// already known. If every remaining step is resolved we land on
// stepDone and the surrounding tea.Program loop tears down.
func (m *setupPickerModel) advancePastResolved() {
	for {
		switch m.step {
		case stepLang:
			if m.needs.Lang {
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
// against the chosen CLI catalog. Falls back to the bundled en pack on
// a missing key so a partial / custom language pack still renders
// reasonable rows instead of bare slugs.
func (m *setupPickerModel) preparePresets() {
	if len(m.presets) > 0 {
		return
	}
	names := installer.SupportedPresets()
	out := make([]presetOption, 0, len(names))
	for _, n := range names {
		out = append(out, presetOption{
			Name:        n,
			Title:       m.translate(fmt.Sprintf("cli.preset.%s.title", n)),
			Description: m.translate(fmt.Sprintf("cli.preset.%s.description", n)),
		})
	}
	m.presets = out
	if idx := indexOfPreset(out, m.inputs.Preset); idx >= 0 {
		m.presetCursor = idx
	}
}

func (m setupPickerModel) translate(key string) string {
	if m.cliCatalog != nil {
		if v, ok := m.cliCatalog.Keys[key]; ok && v != "" {
			return v
		}
	}
	return t(key)
}

// View renders the active screen. The language screen renders only the
// rows + ctrl+c footer — no title — because the user has no catalog
// active yet and additional English chrome would not help the
// non-English caller pick a language. Subsequent screens carry a title
// + hint resolved through the chosen catalog.
func (m setupPickerModel) View() string {
	switch m.step {
	case stepLang:
		return m.renderListView("", "ctrl+c quit"+m.backHint(), langRows(m.langs, m.langCursor, m.styles))
	case stepAgentLang:
		m.agentInput.Placeholder = m.translate("cli.setup.picker.agent.placeholder")
		return m.renderInputView(m.translate("cli.setup.picker.agent.title"), m.translate("cli.setup.picker.hint.input")+m.backHint(), m.agentInput.View())
	case stepPreset:
		return m.renderListView(m.translate("cli.setup.picker.preset.title"), m.translate("cli.setup.picker.hint.nav")+m.backHint(), presetRows(m.presets, m.presetCursor, m.styles))
	case stepHarness:
		return m.renderListView(m.translate("cli.setup.picker.harness.title"), m.translate("cli.setup.picker.hint.multi")+m.backHint(), harnessRows(m.harnesses, m.harnessChosen, m.harnessCursor, m.styles))
	}
	return ""
}

// backHint appends a discoverability hint to the footer once the user
// has at least one screen to go back to. Stays silent on the first
// active step where esc is a no-op so users do not learn a key that
// does nothing.
func (m setupPickerModel) backHint() string {
	if m.activeIndex() > 0 {
		return " · esc back"
	}
	return ""
}

// stepIndicator renders the paginator's "step N/M" using the hint
// style. Returns "" when the active chain has fewer than two steps —
// a 1/1 indicator would just be visual noise.
func (m setupPickerModel) stepIndicator() string {
	if len(m.activeOrder) < 2 || m.activeIndex() < 0 {
		return ""
	}
	return m.styles.hint.Render(m.pager.View())
}

func (m setupPickerModel) renderListView(title, hint string, rows []string) string {
	var b strings.Builder
	b.WriteString("\n")
	if ind := m.stepIndicator(); ind != "" {
		b.WriteString("  " + ind + "\n\n")
	}
	if title != "" {
		b.WriteString(m.styles.title.Render(title))
		b.WriteString("\n\n")
	}
	for _, row := range rows {
		b.WriteString(row + "\n")
	}
	b.WriteString("\n")
	b.WriteString(m.styles.hint.Render(hint))
	b.WriteString("\n")
	return b.String()
}

func (m setupPickerModel) renderInputView(title, hint, field string) string {
	var b strings.Builder
	b.WriteString("\n")
	if ind := m.stepIndicator(); ind != "" {
		b.WriteString("  " + ind + "\n\n")
	}
	b.WriteString(m.styles.title.Render(title))
	b.WriteString("\n\n  ")
	b.WriteString(field)
	b.WriteString("\n\n")
	b.WriteString(m.styles.hint.Render(hint))
	b.WriteString("\n")
	return b.String()
}

func langRows(langs []langOption, cursor int, st setupStyles) []string {
	out := make([]string, 0, len(langs))
	for i, l := range langs {
		label := fmt.Sprintf("%s (%s)", l.Native, l.Code)
		if i == cursor {
			out = append(out, st.marker.Render("› ")+st.rowFocus.Render(label))
		} else {
			out = append(out, "  "+st.row.Render(label))
		}
	}
	return out
}

func presetRows(presets []presetOption, cursor int, st setupStyles) []string {
	out := make([]string, 0, len(presets)*2)
	for i, p := range presets {
		header := fmt.Sprintf("%-10s  %s", p.Name, p.Title)
		if i == cursor {
			out = append(out, st.marker.Render("› ")+st.rowFocus.Render(header))
		} else {
			out = append(out, "  "+st.row.Render(header))
		}
		out = append(out, "    "+st.desc.Render(p.Description))
	}
	return out
}

func harnessRows(harnesses []string, chosen map[int]bool, cursor int, st setupStyles) []string {
	out := make([]string, 0, len(harnesses))
	for i, name := range harnesses {
		box := "[ ]"
		boxStyle := st.box
		if chosen[i] {
			box = "[x]"
			boxStyle = st.boxOn
		}
		label := boxStyle.Render(box) + " " + name
		if i == cursor {
			out = append(out, st.marker.Render("› ")+st.rowFocus.Render(label))
		} else {
			out = append(out, "  "+st.row.Render(label))
		}
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

// loadBundledLanguageOptions enumerates every language pack the picker
// should offer: the bundled `<code>.yaml` files shipped under
// defaults/languages plus any user-authored custom packs at
// `<ConfigRoot>/languages/custom/<code>.yaml`. Custom packs win on a
// code collision (same precedence rule as `config.LoadLanguages` at
// runtime).
//
// Bundled lookup goes through the embed FS so the picker works on a
// fresh install where nothing has been materialized to disk yet. The
// custom lookup is best-effort: a missing dir or a bad YAML emits a
// stderr warning and skips the entry rather than aborting the picker,
// because a broken custom pack should not prevent the user from
// completing the install with the bundled defaults.
func loadBundledLanguageOptions() ([]langOption, error) {
	bundled, err := loadEmbedLanguageOptions()
	if err != nil {
		return nil, err
	}
	customs := loadCustomLanguageOptions()
	return mergeLanguageOptions(bundled, customs), nil
}

func loadEmbedLanguageOptions() ([]langOption, error) {
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
		out = append(out, langOption{Code: code, Native: nativeLabel(lang.Native, lang.Name, code)})
	}
	return out, nil
}

// loadCustomLanguageOptions reads user-authored packs from
// `<ConfigRoot>/languages/custom/`. Returns nil (no error) when the
// directory is missing — that is the expected state on a fresh install
// before `EnsureDefaultFiles` runs. Per-file decode errors print a
// warning and skip the entry.
func loadCustomLanguageOptions() []langOption {
	root, err := paths.ConfigRoot()
	if err != nil {
		return nil
	}
	dir := filepath.Join(root, "languages", "custom")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	type minLang struct {
		Code   string `yaml:"code"`
		Name   string `yaml:"name"`
		Native string `yaml:"native"`
	}
	var out []langOption
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".yaml") && !strings.HasSuffix(lower, ".yml") {
			continue
		}
		path := filepath.Join(dir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: skip %s: %v\n", path, err)
			continue
		}
		var lf minLang
		if err := yaml.Unmarshal(raw, &lf); err != nil {
			fmt.Fprintf(os.Stderr, "warn: skip %s: %v\n", path, err)
			continue
		}
		code := strings.TrimSpace(lf.Code)
		if code == "" || code != strings.ToLower(code) {
			fmt.Fprintf(os.Stderr, "warn: skip %s: invalid code %q\n", path, lf.Code)
			continue
		}
		out = append(out, langOption{Code: code, Native: nativeLabel(lf.Native, lf.Name, code)})
	}
	return out
}

// mergeLanguageOptions merges bundled + custom slices, custom-wins on
// matching codes, and returns the result sorted alphabetically by code
// for deterministic picker order.
func mergeLanguageOptions(bundled, customs []langOption) []langOption {
	byCode := make(map[string]langOption, len(bundled)+len(customs))
	for _, b := range bundled {
		byCode[b.Code] = b
	}
	for _, c := range customs {
		byCode[c.Code] = c
	}
	codes := make([]string, 0, len(byCode))
	for code := range byCode {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	out := make([]langOption, 0, len(codes))
	for _, code := range codes {
		out = append(out, byCode[code])
	}
	return out
}

func nativeLabel(native, name, code string) string {
	if v := strings.TrimSpace(native); v != "" {
		return v
	}
	if v := strings.TrimSpace(name); v != "" {
		return v
	}
	return code
}

// loadBundledTheme reads defaults/themes/omakiten.yaml from the embed
// FS. The installer runs before the user has any on-disk config, so the
// bundled default theme is the only palette available.
func loadBundledTheme() (config.Theme, error) {
	raw, err := defaults.FS.ReadFile("themes/omakiten.yaml")
	if err != nil {
		return config.Theme{}, err
	}
	var theme config.Theme
	if err := yaml.Unmarshal(raw, &theme); err != nil {
		return config.Theme{}, err
	}
	return theme, nil
}

// newSetupStyles builds the lipgloss style set from the theme palette.
// A nil/empty Colors map yields styles with the empty color, which
// lipgloss treats as "inherit terminal default" — safe fallback when
// the embed FS read fails for some reason (e.g. someone strips
// themes/* from the binary).
func newSetupStyles(theme config.Theme) setupStyles {
	primary := lipgloss.Color(theme.Colors["primary"])
	fg := lipgloss.Color(theme.Colors["foreground"])
	secondary := lipgloss.Color(theme.Colors["secondary"])
	success := lipgloss.Color(theme.Colors["success"])
	border := lipgloss.Color(theme.Colors["border"])
	return setupStyles{
		title:    lipgloss.NewStyle().Bold(true).Foreground(primary),
		row:      lipgloss.NewStyle().Foreground(fg),
		rowFocus: lipgloss.NewStyle().Bold(true).Foreground(primary),
		marker:   lipgloss.NewStyle().Bold(true).Foreground(primary),
		hint:     lipgloss.NewStyle().Foreground(secondary),
		desc:     lipgloss.NewStyle().Foreground(secondary),
		box:      lipgloss.NewStyle().Foreground(border),
		boxOn:    lipgloss.NewStyle().Bold(true).Foreground(success),
	}
}

// runSetupPicker launches the bubbletea program when at least one
// picker screen needs the user's input. Returns the populated inputs
// on success, a coded error on ctrl+c, or a validation_error when
// stdin is not a TTY.
func runSetupPicker(ctx context.Context, inputs setupInputs, needs pickerNeeds) (setupInputs, error) {
	if !pickerNeeded(needs) {
		return inputs, nil
	}
	if !stdinIsTTY() {
		return setupInputs{}, domain.NewError(domain.ErrValidation, t("cli.setup.picker.no_tty"), map[string]any{
			"lang_needed":    needs.Lang,
			"agent_needed":   needs.Agent,
			"preset_needed":  needs.Preset,
			"harness_needed": needs.Harness,
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
	if result.aborted || !result.done {
		return setupInputs{}, domain.NewError(domain.ErrValidation, t("cli.setup.picker.aborted"), nil)
	}
	return result.inputs, nil
}

func pickerNeeded(n pickerNeeds) bool {
	return n.Lang || n.Agent || n.Preset || n.Harness
}

func stdinIsTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}
