package main

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/config"
	"omakiten/internal/tui/components/screenkit"
)

func testModel(w, h int) model {
	m := newModel(config.Theme{}, newDemoTheme(config.Theme{}, screenkit.Styles{}))
	m.width, m.height = w, h
	return m.preview()
}

func stroke(spelling string) tea.KeyMsg {
	switch spelling {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(spelling)}
}

func press(t *testing.T, m model, spellings ...string) model {
	t.Helper()
	for _, s := range spellings {
		next, _ := m.handleKey(stroke(s))
		m = next.(model)
	}
	return m
}

func entryIndex(t *testing.T, name string) int {
	t.Helper()
	for i, e := range entries() {
		if e.name == name {
			return i
		}
	}
	t.Fatalf("no entry named %q", name)
	return 0
}

func openEntry(t *testing.T, name string, w, h int) model {
	t.Helper()
	m := testModel(w, h)
	m.sel = entryIndex(t, name)
	m = m.preview()
	m.focus = focusFrame
	return m
}

// focusField puts the cursor on a named property, the way a user does it:
// `tab` to the Properties column, `f` to enter it, then `tab` field by field.
func focusField(t *testing.T, m model, name string) model {
	t.Helper()
	for _, f := range m.fields() {
		if f.name != name {
			continue
		}
		for m.column() != 2 {
			m = m.moveFocus(1)
		}
		if !m.zoneEntered {
			m.zoneEntered = true
			m = m.syncEditor()
		}
		for i := 0; i < len(m.fields()); i++ {
			if got, ok := m.focusedField(); ok && got.name == name {
				return m
			}
			m = m.moveField(1)
		}
		t.Fatalf("walked every field without landing on %q", name)
	}
	t.Fatalf("no property named %q on %s", name, m.currentEntry().name)
	return m
}

// The gallery must be able to outlive the components it inspects. A tool whose
// index is built from cardlist stops running the moment cardlist breaks — which
// is precisely the moment you need it.
func TestTheChromeDoesNotImportWhatItInspects(t *testing.T) {
	chrome := []string{"gallery.go", "chrome.go", "props.go"}
	for _, file := range chrome {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, imp := range parsed.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(path, "omakiten/internal/tui") {
				t.Errorf("%s imports %s — the chrome must be built from bubbles and lipgloss only",
					file, path)
			}
		}
	}
}

// Every demo does arithmetic on the frame size, and the frame is user-driven all
// the way down to the floor. Negative widths panic inside lipgloss, so this
// stands in for a human dragging every knob in every scenario.
func TestEveryScenarioRendersAtEverySize(t *testing.T) {
	sizes := []struct{ w, h int }{{140, 44}, {80, 24}, {minFrameWidth, minFrameHeight}}
	// Component entries only: screen mounts share the golden fixture path and
	// are covered by TestEveryScreenEntryMounts. Sweeping every recorded state
	// at three sizes would re-Drive studio/taskdetail dozens of times per run.
	for i, e := range componentEntries() {
		for s := range e.scenarios {
			m := testModel(150, 50)
			m.sel = i
			m = m.preview().applyScenario(s)
			for _, size := range sizes {
				m.frameProps = m.writeFrame("width", size.w)
				m.frameProps = m.writeFrame("height", size.h)
				m.live = m.live.Resize(m.ctx())
				if frame := m.View(); strings.TrimSpace(frame) == "" {
					t.Fatalf("%s/%s at %dx%d: the host frame is blank",
						e.name, e.scenarios[s].name, size.w, size.h)
				}
			}
		}
	}
}

// TestEveryScreenEntryMounts drives each screenhost surface once through the
// same screenfixture.Drive path the goldens use, so a catalog entry that cannot
// actually mount fails here rather than only looking covered to the AST gate.
func TestEveryScreenEntryMounts(t *testing.T) {
	for i, e := range screenEntries() {
		if e.screen == "" {
			t.Fatalf("screen entry %q has an empty screen id", e.name)
		}
		if len(e.scenarios) == 0 {
			t.Fatalf("screen entry %q (%s) has no recorded scenarios", e.name, e.screen)
		}
		m := testModel(120, 40)
		m.screenMode = true
		m.entries = screenEntries()
		m.sel = i
		m = m.preview()
		if strings.TrimSpace(mustContent(t, m)) == "" {
			t.Fatalf("%s (%s): mounted view is blank", e.name, e.screen)
		}
	}
}

// The gallery is a TUI: a frame wider or taller than the terminal shears the
// whole layout. Every budget it takes exists to keep this true.
func TestTheViewNeverExceedsTheTerminal(t *testing.T) {
	sizes := []struct{ w, h int }{{170, 50}, {150, 44}, {120, 40}, {110, 30}}
	for _, size := range sizes {
		for i, e := range componentEntries() {
			assertEntryFits(t, size.w, size.h, i, e)
		}
	}
}

func assertEntryFits(t *testing.T, width, height, selected int, entry entry) {
	m := testModel(width, height)
	m.sel = selected
	m = m.preview()
	for focus := focusComponents; focus < len(m.fields()); focus++ {
		for _, focused := range []bool{false, true} {
			m.focus = focus
			m.zoneEntered = focused && focus == focusFrame
			assertViewFits(t, m.View(), entry.name, width, height, focus, m.zoneEntered)
		}
	}
}

func assertViewFits(t *testing.T, view, name string, width, height, focus int, focused bool) {
	rows := strings.Split(view, "\n")
	if len(rows) > height {
		t.Errorf("%s at %dx%d focus %d focused=%v: %d rows, terminal has %d", name, width, height, focus, focused, len(rows), height)
	}
	for n, row := range rows {
		if got := lipgloss.Width(row); got > width {
			t.Errorf("%s at %dx%d focus %d focused=%v: row %d is %d cols, terminal has %d", name, width, height, focus, focused, n, got, width)
		}
	}
}

func TestTheFrameAlwaysPreviewsTheSelection(t *testing.T) {
	m := testModel(140, 44)
	if m.live == nil || m.focus != focusComponents {
		t.Fatal("a fresh gallery should preview the first component from the index")
	}
	if strings.TrimSpace(mustContent(t, m)) == "" {
		t.Error("the preview frame is blank before enter")
	}
	first := m.live.Status(m.ctx())
	m = press(t, m, "j")
	if m.sel != 1 {
		t.Fatalf("j selected %d, want 1", m.sel)
	}
	if m.live.Status(m.ctx()) == first && m.currentEntry().name == entries()[0].name {
		t.Error("moving the index did not swap the previewed component")
	}
}

// A scenario is a preset: one pick puts the frame AND the component into a whole
// situation at once.
func TestAScenarioSeedsTheFrameAndTheComponent(t *testing.T) {
	m := openEntry(t, "list", 160, 50)
	target := -1
	for i, s := range m.scenarios() {
		if s.width > 0 && len(s.props) == 0 {
			target = i
		}
	}
	if target < 0 {
		t.Skip("list has no frame-only scenario")
	}
	m = m.applyScenario(target)
	if got, want := m.frameW(), m.scenarios()[target].width; got != want {
		t.Errorf("the scenario set the width to %d, want %d", got, want)
	}

	// And one that reaches the component's own properties.
	m = openEntry(t, "list", 160, 50)
	for i, s := range m.scenarios() {
		if s.name == "empty" {
			m = m.applyScenario(i)
			break
		}
	}
	if got, _ := findProp(m.live.Props(), "items"); got.number != 0 {
		t.Errorf("the scenario left items at %d, want 0", got.number)
	}
	if !strings.Contains(m.live.Status(m.ctx()), "empty") {
		t.Errorf("the component did not go empty: %q", m.live.Status(m.ctx()))
	}
}

// It SEEDS and does not lock: everything a scenario wrote stays editable.
func TestAScenarioDoesNotLockTheProperties(t *testing.T) {
	m := openEntry(t, "list", 160, 50)
	for i, s := range m.scenarios() {
		if s.props["items"] == "0" {
			m = m.applyScenario(i)
		}
	}
	m = focusField(t, m, "items")
	m = press(t, m, "7")
	if got, _ := findProp(m.live.Props(), "items"); got.number != 7 {
		t.Fatalf("items is %d after typing over the scenario, want 7", got.number)
	}
}

// The properties are the component's own inputs, so editing one at runtime has
// to change what the component was asked to do.
func TestEditingAPropertyReachesTheComponent(t *testing.T) {
	m := focusField(t, openEntry(t, "list", 160, 50), "viewport")
	m = press(t, m, "6")
	if got, _ := findProp(m.live.Props(), "viewport"); got.number != 6 {
		t.Fatalf("viewport is %d, want 6", got.number)
	}
	if !strings.Contains(m.live.Status(m.ctx()), "visible") {
		t.Fatalf("status lost its window read-out: %q", m.live.Status(m.ctx()))
	}

	// A text property too — the card title is a real argument.
	m = focusField(t, m, "label")
	for i := 0; i < 30; i++ {
		m = press(t, m, "backspace")
	}
	m = press(t, m, "x", "y")
	if got, _ := findProp(m.live.Props(), "label"); got.text != "xy" {
		t.Errorf("label is %q, want %q", got.text, "xy")
	}
	if !strings.Contains(mustContent(t, m), "xy") {
		t.Error("the frame does not show the edited label")
	}
}

func TestEveryComponentDeclaresRealProperties(t *testing.T) {
	theme := newDemoTheme(config.Theme{}, screenkit.Styles{})
	for _, e := range componentEntries() {
		d := e.new(theme.ctx(80, 24))
		if len(d.Props()) == 0 {
			t.Errorf("%s exposes no properties", e.name)
		}
		for _, p := range d.Props() {
			if strings.TrimSpace(p.note) == "" {
				t.Errorf("%s: property %q has no note saying what it does", e.name, p.name)
			}
			if p.kind == propChoice && len(p.options) < 2 {
				t.Errorf("%s: choice %q offers %d options", e.name, p.name, len(p.options))
			}
		}
		if len(e.scenarios) < 2 {
			t.Errorf("%s declares %d scenarios, want at least two to be worth a column", e.name, len(e.scenarios))
		}
	}
}

// TestEveryScreenEntryHasScenarios is the screen half of the property gate:
// one recorded golden state is enough, and the state picker is the only prop.
func TestEveryScreenEntryHasScenarios(t *testing.T) {
	theme := newDemoTheme(config.Theme{}, screenkit.Styles{})
	for _, e := range screenEntries() {
		if e.screen == "" {
			t.Errorf("%s is in screenEntries but has no screen id", e.name)
		}
		if len(e.scenarios) == 0 {
			t.Errorf("%s declares no scenarios", e.name)
		}
		d := e.new(theme.ctx(80, 24))
		if len(d.Props()) == 0 {
			t.Errorf("%s exposes no properties", e.name)
		}
	}
}

// The ring follows the columns' order on screen: scenario, frame, then the
// Properties panel. Walking against the layout is the kind of surprise the tool
// exists to catch elsewhere.
func TestTabWalksTheColumnsInReadingOrder(t *testing.T) {
	m := openEntry(t, "list", 160, 50)
	m.focus = focusScenarios

	m = press(t, m, "tab")
	if m.focus != focusFrame {
		t.Fatalf("the first tab landed on %d, want the frame", m.focus)
	}
	// The Properties COLUMN is one stop, not one stop per field. Walking the
	// fields would make the presses between two columns depend on how many
	// properties the component happens to declare.
	m = press(t, m, "tab")
	if m.column() != 2 {
		t.Fatalf("the second tab landed on column %d, want the properties", m.column())
	}
	if m.zoneEntered {
		t.Error("arriving at a column is not entering it")
	}
	m = press(t, m, "tab")
	if m.focus != focusScenarios {
		t.Fatalf("the third tab did not return to the scenarios, landed on %d", m.focus)
	}
	m = press(t, m, "shift+tab")
	if m.column() != 2 {
		t.Fatalf("shift+tab landed on column %d, want the properties", m.column())
	}
}

// THE STANDARD: the arrows move inside the focused column whether or not it has
// been entered. A column you are on is a column you can look through, and the
// two columns that have children must not disagree about how you look through
// them.
func TestTheArrowsWalkAColumnWithoutEnteringIt(t *testing.T) {
	m := openEntry(t, "list", 160, 50)

	// Scenarios: the arrows move the selection and the column is never entered.
	m.focus = focusScenarios
	before := m.scenario
	m = press(t, m, "down")
	if m.scenario == before {
		t.Error("down on the scenarios did not move the selection")
	}
	if m.zoneEntered {
		t.Error("the arrows must not enter the column")
	}

	// Properties: the same, on a column that used to need entering first.
	for m.column() != 2 {
		m = m.moveFocus(1)
	}
	first := m.focus
	m = press(t, m, "down")
	if m.focus == first {
		t.Fatal("down on the properties did not move to the next field")
	}
	if m.zoneEntered {
		t.Error("the arrows must not enter the column")
	}
	if m = press(t, m, "up"); m.focus != first {
		t.Errorf("up landed on field %d, want back on %d", m.focus, first)
	}
}

// What entering buys is the keys the CHROME owns and the ones that mean text —
// never the arrows, which work either way.
func TestEnteringOnlyChangesTheKeysTheChromeOwns(t *testing.T) {
	m := openEntry(t, "list", 160, 50)
	for m.column() != 2 {
		m = m.moveFocus(1)
	}
	// Unentered, `j` walks the fields like an arrow does.
	first := m.focus
	if walked := press(t, m, "j"); walked.focus == first {
		t.Error("unentered, j should walk the fields")
	}

	m = press(t, m, "f")
	if !m.zoneEntered {
		t.Fatal("f should have entered the column")
	}
	// Entered, `j` is a letter: it belongs to whatever is being typed into.
	at := m.focus
	if typed := press(t, m, "j"); typed.focus != at {
		t.Error("entered, j is text and must not walk the fields")
	}
	// The arrows still walk, entered or not.
	if moved := press(t, m, "down"); moved.focus == at {
		t.Error("entered, the arrows should still walk the fields")
	}
}

// `f` on the Properties column is what puts `tab` on its fields, and `esc`
// hands `tab` back to the columns. It is the same three keys the frame uses and
// the same three the grid uses inside a body.
func TestFEntersThePropertiesColumnAndEscLeavesIt(t *testing.T) {
	m := openEntry(t, "list", 160, 50)
	for m.column() != 2 {
		m = press(t, m, "tab")
	}
	first := m.focus

	m = press(t, m, "f")
	if !m.zoneEntered {
		t.Fatal("f on the properties column should have entered it")
	}
	m = press(t, m, "tab")
	if m.focus == first {
		t.Error("entered, tab should walk the fields")
	}
	if m.column() != 2 {
		t.Error("entered, tab must not leave the column")
	}

	m = press(t, m, "esc")
	if m.zoneEntered {
		t.Fatal("esc should have left the column")
	}
	m = press(t, m, "tab")
	if m.focus != focusScenarios {
		t.Errorf("after leaving, tab landed on %d, want the next column", m.focus)
	}
}

// Entering a component lands on the scenarios: picking one is how an inspection
// starts, and every pick previews itself as the cursor moves.
func TestEnterLandsOnTheScenarios(t *testing.T) {
	m := press(t, testModel(160, 50), "enter")
	if m.focus != focusScenarios {
		t.Fatalf("enter landed on %d, want the scenario column", m.focus)
	}
	before := m.frameW()
	// A scenario wider than the pane clamps back to the pane, so look for one
	// that actually lands on a different number.
	target := -1
	for i, sc := range m.scenarios() {
		if sc.width > 0 && sc.width < m.maxFrameWidth() && sc.width != before {
			target = i
		}
	}
	if target <= 0 {
		t.Skip("the first component has no scenario that narrows the frame")
	}
	for i := 0; i < target; i++ {
		m = press(t, m, "j")
	}
	if m.scenario != target {
		t.Fatalf("j walked to scenario %d, want %d", m.scenario, target)
	}
	if m.frameW() == before {
		t.Error("moving through the scenarios did not preview them")
	}
}

// Shrinking the frame must not drag the Properties column left to chase it:
// comparing two geometries is impossible if everything else moves too.
func TestTheFrameContainerHoldsItsWidth(t *testing.T) {
	m := openEntry(t, "list", 160, 50)
	wide := lipgloss.Width(m.frameContainer())
	m.frameProps = m.writeFrame("width", 24)
	m = m.clampFrame()
	if narrow := lipgloss.Width(m.frameContainer()); narrow != wide {
		t.Errorf("the container is %d columns with a narrow frame and %d with a wide one", narrow, wide)
	}

	// And the Properties column keeps its left edge.
	edge := func(mm model) int {
		for _, row := range strings.Split(mm.View(), "\n") {
			if idx := strings.Index(row, "PROPERTIES"); idx >= 0 {
				return idx
			}
		}
		return -1
	}
	m.frameProps = m.writeFrame("width", m.maxFrameWidth())
	if got, want := edge(m.clampFrame()), edge(m); got != want {
		t.Errorf("the Properties column moved from column %d to %d when the frame resized", want, got)
	}
}

// The columns beside the frame stand as tall as the screen, not as tall as the
// simulated frame — otherwise every small geometry leaves a dead band under them.
func TestTheColumnsFillTheScreenHeight(t *testing.T) {
	m := openEntry(t, "list", 160, 50)
	m.frameProps = m.writeFrame("height", 6)
	m = m.clampFrame()

	want := m.panelRows()
	for name, panel := range map[string]string{
		"scenario":   m.scenarioPanel(),
		"properties": m.propertiesPanel(),
		"container":  m.frameContainer(),
	} {
		if got := lipgloss.Height(panel); got != want {
			t.Errorf("%s is %d rows tall with a 6-row frame, want %d", name, got, want)
		}
	}
	if got, want := lipgloss.Height(m.componentsPanel()), m.bodyRows(); got != want {
		t.Errorf("the components column is %d rows, want the whole body %d", got, want)
	}
}

func TestFrameKeysReachTheComponent(t *testing.T) {
	m := openEntry(t, "list", 160, 50)
	before := m.live.Status(m.ctx())
	m = press(t, m, "j", "j")
	if after := m.live.Status(m.ctx()); after == before {
		t.Fatalf("j did not move the component: status stayed %q", before)
	}
}

// Padding is inside the frame, so it comes out of the component's geometry.
func TestPaddingComesOutOfTheComponentsGeometry(t *testing.T) {
	m := focusField(t, openEntry(t, "list", 160, 50), "padding")
	w, h := m.frameW(), m.frameH()
	m = press(t, m, "3")
	if m.padding() != 3 {
		t.Fatalf("padding is %d, want 3", m.padding())
	}
	if m.frameW() != w || m.frameH() != h {
		t.Errorf("padding changed the frame to %dx%d, want %dx%d", m.frameW(), m.frameH(), w, h)
	}
	if got, want := m.ctx().kit.Width, w-6; got != want {
		t.Errorf("the component was told %d columns, want %d", got, want)
	}
}

func TestAlignmentMovesTheContentInsideTheFrame(t *testing.T) {
	m := openEntry(t, "tokenstrip · keys", 160, 50)
	m.frameProps = m.writeFrame("height", 12)
	m = m.clampFrame()

	if strings.TrimSpace(strings.Split(mustContent(t, m), "\n")[0]) == "" {
		t.Fatal("with align top the first row should carry the component")
	}
	m = focusField(t, m, "align ↕")
	m = press(t, m, "right", "right") // top -> middle -> bottom
	rows := strings.Split(mustContent(t, m), "\n")
	if strings.TrimSpace(rows[0]) != "" {
		t.Error("with align bottom the first row should be blank")
	}
	if strings.TrimSpace(rows[len(rows)-1]) == "" {
		t.Error("with align bottom the last row should carry the component")
	}
}

// The chrome and the components both want `tab`, and the component has to win —
// screenlayout's key table contains it, so every screen built on the arranger
// does too. A gallery that keeps `tab` for itself can only ever drive a key
// table the app does not ship.
func TestFocusModeHandsTabToTheComponent(t *testing.T) {
	m := openEntry(t, "shell", 160, 50)
	if m.zoneEntered {
		t.Fatal("the frame should not start focused — entering focus is a deliberate act")
	}

	// Unfocused, tab is the chrome's: it walks to the Properties panel.
	if walked := press(t, m, "tab"); walked.focus == focusFrame {
		t.Error("unfocused, tab should leave the frame for the next column")
	}

	m = press(t, m, "f")
	if !m.zoneEntered {
		t.Fatal("f on the frame should focus the component")
	}

	before := m.live.Status(m.ctx())
	m = press(t, m, "tab")
	if m.focus != focusFrame {
		t.Fatal("focused, tab must not move the chrome's focus")
	}
	if m.live.Status(m.ctx()) == before {
		t.Error("focused, tab should have reached the component and moved its focus")
	}
}

// `f` is the app's own spelling for focusing a zone, and inside focus mode it
// stops being the chrome's — otherwise "every key reaches the component" has an
// exception, and the one key it excepts is the one the app uses most.
func TestFEntersFocusAndThenBelongsToTheComponent(t *testing.T) {
	m := press(t, openEntry(t, "shell", 160, 50), "f")
	if !m.zoneEntered {
		t.Fatal("f should have entered focus mode")
	}
	if again := press(t, m, "f"); !again.zoneEntered {
		t.Error("f inside focus mode is the component's key, not a toggle back out")
	}
}

// A focus that cannot be left is worse than one that cannot be entered.
func TestEscapeAlwaysLeavesFocusMode(t *testing.T) {
	m := press(t, openEntry(t, "shell", 160, 50), "f")
	if !m.zoneEntered {
		t.Fatal("f should have focused the component")
	}
	m = press(t, m, "esc")
	if m.zoneEntered {
		t.Fatal("esc should leave focus mode")
	}
	if m.focus != focusFrame {
		t.Error("the first esc leaves focus mode and stays on the frame; the second one leaves the frame")
	}
	if m = press(t, m, "esc"); m.focus != focusComponents {
		t.Error("a second esc should leave the frame")
	}
}

// Focus is held over one component in one column. Neither the column nor the
// subject may change while the keyboard is pointed somewhere the user did not
// aim it.
func TestFocusModeNeverOutlivesItsSubject(t *testing.T) {
	m := press(t, openEntry(t, "shell", 160, 50), "f")
	if moved := m.moveFocus(1); moved.zoneEntered {
		t.Error("leaving the frame should leave focus mode")
	}
	if swapped := m.preview(); swapped.zoneEntered {
		t.Error("swapping the component should leave focus mode")
	}
}

// Every demo's advertised keys must be reachable, which in focus mode means
// none of them may be a key the chrome keeps for itself.
func TestNoDemoAdvertisesAKeyTheChromeKeepsInFocusMode(t *testing.T) {
	// esc leaves and ctrl+c quits; nothing else is reserved.
	reserved := map[string]bool{"esc": true, "ctrl+c": true}
	theme := newDemoTheme(config.Theme{}, screenkit.Styles{})
	for _, e := range entries() {
		d := e.new(theme.ctx(120, 40))
		for _, b := range d.Help() {
			for _, k := range b.Keys() {
				if reserved[k] {
					t.Errorf("%s advertises %q, which focus mode never delivers", e.name, k)
				}
			}
		}
	}
}

// Dragging the frame below the width every zone declares is the case the shell
// exists to answer: three zones at a third of the width they asked for are three
// zones you cannot read, so the body collapses to the focused one and `tab` is
// what the others are behind.
func TestTheShellCollapsesToOneZoneOnANarrowFrame(t *testing.T) {
	m := openEntry(t, "shell", 160, 50)
	m.frameProps = m.writeFrame("width", 37)
	m.frameProps = m.writeFrame("height", 23)
	m = m.clampFrame()

	content := mustContent(t, m)
	zones := 0
	for _, id := range []string{"details", "subtasks", "activity"} {
		if strings.Contains(strings.ToLower(content), id) {
			zones++
		}
	}
	if zones != 1 {
		t.Fatalf("%d of the three zones are on a 33-column body, want only the focused one:\n%s", zones, content)
	}
	if !strings.Contains(strings.ToLower(content), "details") {
		t.Error("the zone on screen should be the focused one")
	}
	if !strings.Contains(content, "▼") {
		t.Error("the zones it hid should be counted")
	}

	// And the collapsed body follows the focus rather than stranding it.
	// tab walks the zones the LAYOUT drew. Collapsed, the left column has been
	// dissolved, so the three zones on screen are three stops and the next one
	// is the sub-task board — not the feed the tree lists second.
	m = press(t, m, "f", "tab")
	if got := strings.ToLower(mustContent(t, m)); !strings.Contains(got, "subtasks") {
		t.Errorf("after tab the collapsed body should show the next zone:\n%s", got)
	}
}

func mustContent(t *testing.T, m model) string {
	t.Helper()
	content, _ := m.frameContent()
	return content
}

func TestOverflowIsClippedAndCounted(t *testing.T) {
	m := openEntry(t, "list", 160, 50)
	m.frameProps = m.writeFrame("height", 4)
	m = m.clampFrame()
	content, _ := m.frameContent()
	if lines := strings.Count(content, "\n") + 1; lines != m.frameH() {
		t.Fatalf("frame rendered %d rows, want exactly %d", lines, m.frameH())
	}
	if got := m.statusLine(3); !strings.Contains(got, "+3 rows clipped") {
		t.Errorf("status line %q does not report the clip", got)
	}
}

func TestDumpCoversEveryScenario(t *testing.T) {
	out := dumpAll(config.Theme{}, newDemoTheme(config.Theme{}, screenkit.Styles{}), 100, 34)
	for _, e := range entries() {
		if !strings.Contains(out, e.title) {
			t.Errorf("dump is missing %s", e.name)
		}
		for _, s := range e.scenarios {
			if !strings.Contains(out, s.name) {
				t.Errorf("dump is missing %s/%s", e.name, s.name)
			}
		}
	}
}

func TestQuitKeys(t *testing.T) {
	if _, cmd := testModel(120, 40).handleKey(stroke("q")); cmd == nil {
		t.Error("q did not quit from the index")
	}
	if _, cmd := testModel(120, 40).handleKey(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Error("ctrl+c did not quit")
	}
}

// A scenario is a shortcut to a starting point, not a mode — so arriving at one
// has to paint the same bytes whether you got there first or after walking the
// whole list.
//
// It did not. applyScenario seeded only the props a scenario names, on top of
// whatever the previous one left, so every silent prop carried over. The review
// caught it on five entries at once: cardlist's `board lane` came back empty
// having inherited `items: 0`, screenlayout's `wide desktop` came back as the
// placements table having inherited `body: placements`.
//
// This walks every component entry rather than the five, because the defect was
// never about those five — it was about the seeding, which every entry shares.
func TestAScenarioPaintsTheSameOnArrivalAndOnReturn(t *testing.T) {
	checked := 0
	for idx, e := range entries() {
		if e.screen != "" || len(e.scenarios) < 2 {
			continue
		}
		m := testModel(200, 60)
		m.sel = idx
		m = m.preview()

		first := m.live.View(m.ctx())
		for i := 1; i < len(e.scenarios); i++ {
			m = m.applyScenario(i)
			_ = m.live.View(m.ctx()) // paint it, exactly as the eye would
		}
		m = m.applyScenario(0)

		if got := m.live.View(m.ctx()); got != first {
			t.Errorf("%s: scenario %q differs after visiting the others\n--- on arrival ---\n%s\n--- on return ---\n%s",
				e.name, e.scenarios[0].name, first, got)
		}
		checked++
	}
	if checked < 15 {
		t.Fatalf("only %d entries checked — the gate is vacuously green", checked)
	}
}
