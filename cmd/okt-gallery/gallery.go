package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	bubbleviewport "github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/config"
	"omakiten/internal/keynav"
)

const (
	// Outer widths of the three panels flanking the frame.
	componentsWidth = 26
	scenarioWidth   = 22
	propsWidth      = 30
	paneGap         = 1
	frameBorders    = 2
	statusRows      = 1

	minFrameWidth  = 8
	minFrameHeight = 3
	maxPadding     = 12
)

// Focus positions before the property fields, which occupy 0..n-1. Their order
// is the columns' order on screen — scenario, frame, then the panel — so tab
// walks left to right instead of against the layout.
const (
	focusComponents = -3
	focusScenarios  = -2
	focusFrame      = -1
)

type model struct {
	theme  config.Theme
	styles chromeStyles
	// demos is the bridge to the components' own theme. The gallery never paints
	// itself with it — it only hands it to the subject.
	demos demoTheme

	entries  []entry
	sel      int
	scenario int
	focus    int
	live     demo
	// screenMode switches the left index between component entries and the
	// screenhost surfaces. Both catalogs live in entries()/screenEntries();
	// the model holds only the active half so j/k walk one mode at a time.
	screenMode bool

	// zoneEntered is FOCUS MODE: the focused COLUMN owns the keyboard, and `tab`
	// is one of the keys it owns.
	//
	// Without it the chrome permanently owns `tab`, and a component whose own key
	// table contains `tab` — which screenlayout's does, and therefore so does
	// every screen built on it — can never be driven the way its users drive it.
	// Remapping the collision per demo was the first attempt and it is wrong
	// twice over: the gallery would be exercising a key table that does not ship,
	// and the remap has to be reinvented for every component that collides.
	//
	// So the chrome gives up the keyboard instead of the component giving up its
	// keys. The vocabulary is the APP'S, not one invented here: `f` focuses a
	// zone and `esc` leaves it, which is what task detail and project already
	// spell as `f · esc` on their own footers.
	//
	// It is the same rule the grid uses, at every depth: `tab` walks SIBLINGS and
	// never descends, `f` enters what is focused, `esc` comes back out. Without
	// it `tab` walks the three columns and then keeps going into the Properties
	// column's fields — so the number of presses to get from the frame back to
	// the scenarios depends on how many properties the component happens to
	// declare, which is not a thing the user should have to know.
	//
	// While entered, every keystroke goes to the column except the two that
	// cannot be surrendered: `esc` leaves, and ctrl+c quits.
	zoneEntered bool

	// frameProps are the simulated terminal's own inputs, shown above the
	// component's. They are ordinary props so one editor serves both.
	frameProps []prop

	editor textinput.Model
	help   help.Model

	width, height int
}

func newModel(theme config.Theme, demos demoTheme) model {
	editor := textinput.New()
	editor.Prompt = ""
	editor.CharLimit = 64

	helper := help.New()
	helper.ShowAll = false

	m := model{
		theme:      theme,
		styles:     newChromeStyles(theme),
		demos:      demos,
		entries:    componentEntries(),
		focus:      focusComponents,
		frameProps: newFrameProps(),
		editor:     editor,
		help:       helper,
		width:      120,
		height:     40,
	}
	return m.preview()
}

func newFrameProps() []prop {
	return []prop{
		numberProp("height", "rows the simulated terminal has", 0, minFrameHeight, 400),
		numberProp("width", "columns the simulated terminal has", 0, minFrameWidth, 400),
		numberProp("padding", "blank rows and columns inside the frame", 0, 0, maxPadding),
		choiceProp("align ↕", "where the component sits when it is shorter than the frame", 0, "top", "middle", "bottom"),
		choiceProp("align ↔", "where it sits when it is narrower", 0, "left", "center", "right"),
	}
}

// ---- state -----------------------------------------------------------------

func (m model) currentEntry() entry { return m.entries[clamp(m.sel, 0, len(m.entries)-1)] }

func (m model) scenarios() []scenario { return m.currentEntry().scenarios }

func (m model) frameW() int  { return propNumber_(m.frameProps, "width") }
func (m model) frameH() int  { return propNumber_(m.frameProps, "height") }
func (m model) padding() int { return propNumber_(m.frameProps, "padding") }

func (m model) innerWidth() int  { return maxInt(m.frameW()-2*m.padding(), 1) }
func (m model) innerHeight() int { return maxInt(m.frameH()-2*m.padding(), 1) }

// ctx is the geometry a demo renders against: the frame minus its padding, never
// the terminal. Everything downstream believes it is on a terminal that size.
func (m model) ctx() demoCtx { return m.demos.ctx(m.innerWidth(), m.innerHeight()) }

// preview instantiates the selected component, applies its first scenario and
// sizes the frame. Moving through the index calls it, so the frame always shows
// what the index points at — seeing a component costs no keystroke.
func (m model) preview() model {
	m.scenario = 0
	// Focus is held over ONE component. Swapping the subject out from under it
	// would leave the keyboard pointed at something the user did not enter.
	m.zoneEntered = false
	m.frameProps = newFrameProps()
	m.frameProps = m.writeFrame("width", m.maxFrameWidth())
	m.frameProps = m.writeFrame("height", m.maxFrameHeight())
	m.live = m.currentEntry().new(m.ctx())
	// Re-take the budget with the component in hand: it lengthens the Properties
	// panel and widens the footer, both of which move the pane.
	m.frameProps = m.writeFrame("width", m.maxFrameWidth())
	m.frameProps = m.writeFrame("height", m.maxFrameHeight())
	return m.applyScenario(0)
}

func (m model) writeFrame(name string, value int) []prop {
	p, ok := findProp(m.frameProps, name)
	if !ok {
		return m.frameProps
	}
	p.number = clamp(value, p.min, p.max)
	return replaceProp(m.frameProps, p)
}

// applyScenario seeds the frame AND the component from a preset, then leaves
// every value editable. A scenario is a shortcut to a starting point, not a mode.
func (m model) applyScenario(idx int) model {
	all := m.scenarios()
	if len(all) == 0 {
		return m.clampFrame()
	}
	m.scenario = clamp(idx, 0, len(all)-1)
	s := all[m.scenario]

	// An omitted axis is "whatever the frame holds", not "whatever the last
	// scenario asked for". Falling through without writing leaves the previous
	// scenario's geometry in place, which is the same leak as the props below and
	// hides in exactly the same way: right on arrival, wrong on return.
	if s.width > 0 {
		m.frameProps = m.writeFrame("width", s.width)
	} else {
		m.frameProps = m.writeFrame("width", m.maxFrameWidth())
	}
	if s.height > 0 {
		m.frameProps = m.writeFrame("height", s.height)
	} else {
		m.frameProps = m.writeFrame("height", m.maxFrameHeight())
	}
	m.frameProps = m.writeFrame("padding", s.padding)
	if s.vertical != "" {
		m.frameProps = replaceProp(m.frameProps, mustProp(m.frameProps, "align ↕").withRaw(s.vertical))
	}
	if s.horizontal != "" {
		m.frameProps = replaceProp(m.frameProps, mustProp(m.frameProps, "align ↔").withRaw(s.horizontal))
	}
	// Rebuild the component from its declared defaults BEFORE seeding.
	//
	// Without this the seeding is a diff against whatever the previous scenario
	// left behind: a scenario only writes the props it names, so every prop it
	// stays silent about keeps the last value some other scenario chose. The
	// symptom is that a scenario renders correctly on arrival — preview() builds
	// it fresh — and differently when you come back to it, which is what the
	// component review found on five entries at once. cardlist's `board lane`
	// came back empty because it inherited `items: 0` from `empty`; screenlayout's
	// `wide desktop` came back as the placements table because it inherited
	// `body: placements`.
	//
	// The doc comment above already promised this: a scenario is a shortcut to a
	// STARTING POINT. A starting point that depends on where you have been is a
	// mode, which is the thing it says it is not.
	m.live = m.currentEntry().new(m.ctx())
	if len(s.props) > 0 {
		for _, p := range seed(m.live.Props(), s.props) {
			m.live = m.live.SetProp(p, m.ctx())
		}
	}
	return m.clampFrame()
}

func mustProp(props []prop, name string) prop {
	p, _ := findProp(props, name)
	return p
}

func (m model) clampFrame() model {
	m.frameProps = m.writeFrame("width", clamp(m.frameW(), minFrameWidth, m.maxFrameWidth()))
	m.frameProps = m.writeFrame("height", clamp(m.frameH(), minFrameHeight, m.maxFrameHeight()))
	m.frameProps = m.writeFrame("padding", clamp(m.padding(), 0, m.maxPadding()))
	m.live = m.live.Resize(m.ctx())
	return m
}

func (m model) maxPadding() int {
	room := minInt((m.frameW()-minFrameWidth)/2, (m.frameH()-minFrameHeight)/2)
	return clamp(room, 0, maxPadding)
}

// ---- fields ----------------------------------------------------------------

// fields is the Properties column in tab order: the frame's own inputs first,
// then the component's, under a rule.
//
// It carries no budget. The footer names the focused field, the body budget
// measures the footer, and the budget is derived from the body — a field list
// that knew its own maximum would make asking "which field has focus?" ask for a
// budget still being computed.
func (m model) fields() []prop {
	return append(append([]prop{}, m.frameProps...), m.live.Props()...)
}

func (m model) frameFieldCount() int { return len(m.frameProps) }

func (m model) focusedField() (prop, bool) {
	fields := m.fields()
	if m.focus < 0 || m.focus >= len(fields) {
		return prop{}, false
	}
	return fields[m.focus], true
}

// writeField routes an edited prop back to whichever set owns it. The gallery
// never keeps a second copy of a component's own state.
func (m model) writeField(p prop) model {
	if m.focus < m.frameFieldCount() {
		m.frameProps = replaceProp(m.frameProps, p)
		return m.clampFrame()
	}
	m.live = m.live.SetProp(p, m.ctx())
	return m
}

// ---- update ----------------------------------------------------------------

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m.clampFrame(), nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch m.focus {
	case focusComponents:
		return m.handleComponentsKey(msg)
	case focusFrame:
		return m.handleFrameKey(msg)
	case focusScenarios:
		return m.handleScenarioKey(msg)
	}
	return m.handleFieldKey(msg)
}

func (m model) handleComponentsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	before := m.sel
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "enter", "l", "right":
		// Land on the scenarios: picking one is how you start, and every pick
		// previews itself as you move.
		m.focus = focusScenarios
		return m.clampFrame(), nil
	case "m":
		m.screenMode = !m.screenMode
		if m.screenMode {
			m.entries = screenEntries()
		} else {
			m.entries = componentEntries()
		}
		m.sel = 0
		return m.preview(), nil
	case "j", "down":
		m.sel = minInt(m.sel+1, len(m.entries)-1)
	case "k", "up":
		m.sel = maxInt(m.sel-1, 0)
	case "g", "home":
		m.sel = 0
	case "G", "end":
		m.sel = len(m.entries) - 1
	}
	if m.sel != before {
		m = m.preview()
	}
	return m, nil
}

func (m model) handleFrameKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Focused: the component has the keyboard, `esc` is the way back out, and
	// ctrl+c was already taken before this function ran. `f` is NOT a toggle
	// here — inside focus mode it is the component's key like any other, which
	// is what "every key reaches it" has to mean to be worth having.
	if m.zoneEntered {
		if msg.Type == tea.KeyEsc {
			m.zoneEntered = false
			return m, nil
		}
		m.live = m.live.Update(msg, m.ctx())
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEsc:
		m.focus = focusComponents
		return m, nil
	case tea.KeyTab:
		return m.moveFocus(1), nil
	case tea.KeyShiftTab:
		return m.moveFocus(-1), nil
	}
	if keynav.Resolve(msg.String(), false) == keynav.Enter {
		m.zoneEntered = true
		return m, nil
	}
	// Unfocused, the chrome still forwards everything it does not use itself.
	// Focus mode is for the keys it DOES use; it is not a mode you have to enter
	// before `j` does anything.
	m.live = m.live.Update(msg, m.ctx())
	return m, nil
}

func (m model) handleScenarioKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.focus = focusComponents
		return m, nil
	case tea.KeyTab:
		return m.moveFocus(1), nil
	case tea.KeyShiftTab:
		return m.moveFocus(-1), nil
	}
	// The scenarios column holds no text and no child that wants `tab`, so it
	// is never entered: `false` is the honest argument, not a shortcut.
	switch keynav.Resolve(msg.String(), false) {
	case keynav.NextChild:
		return m.applyScenario(m.scenario + 1), nil
	case keynav.PrevChild:
		return m.applyScenario(m.scenario - 1), nil
	}
	if msg.String() == "enter" {
		return m.applyScenario(m.scenario), nil
	}
	return m, nil
}

func (m model) handleFieldKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	field, ok := m.focusedField()
	if !ok {
		return m, nil
	}
	// The ARROWS move inside the focused column whether or not it has been
	// entered. That is the standard across the whole tool: a column you are on
	// is a column you can look through, and the scenarios have always worked
	// this way. What entering buys is the keys the CHROME owns — `tab` — and the
	// ones that mean text.
	//
	// `j` / `k` move too, but only until the column is entered: after that they
	// are letters, and a field being typed into owns every letter.
	switch keynav.Resolve(msg.String(), m.zoneEntered) {
	case keynav.PrevChild:
		return m.moveField(-1), nil
	case keynav.NextChild:
		return m.moveField(1), nil
	}

	// The Properties COLUMN is one stop for `tab` until it is entered, and no
	// key reaches the editor before then — otherwise passing through the column
	// on the way to the scenarios would type into whatever field happened to be
	// under the cursor.
	if !m.zoneEntered {
		return m.handleUnenteredFieldKey(msg)
	}
	switch msg.Type {
	case tea.KeyEsc:
		m.zoneEntered = false
		m.editor.Blur()
		return m, nil
	case tea.KeyTab:
		return m.moveField(1), nil
	case tea.KeyShiftTab:
		return m.moveField(-1), nil
	}
	if field.kind == propChoice {
		switch msg.String() {
		case "right", "l", " ", "enter":
			return m.writeField(field.cycle(1)), nil
		case "left", "h":
			return m.writeField(field.cycle(-1)), nil
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	return m.writeField(field.withRaw(m.editor.Value())), cmd
}

func (m model) handleUnenteredFieldKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.focus = focusComponents
		m.editor.Blur()
		return m, nil
	case tea.KeyTab:
		return m.moveFocus(1), nil
	case tea.KeyShiftTab:
		return m.moveFocus(-1), nil
	}
	if keynav.Resolve(msg.String(), false) == keynav.Enter {
		m.zoneEntered = true
		return m.syncEditor(), nil
	}
	return m, nil
}

// column is which of the three the focus is on: 0 scenarios, 1 frame,
// 2 properties. A field index is the Properties COLUMN, not a fourth stop.
func (m model) column() int {
	switch m.focus {
	case focusScenarios:
		return 0
	case focusFrame:
		return 1
	default:
		return 2
	}
}

// moveFocus walks the three COLUMNS: scenario → frame → properties → scenario.
//
// It does not descend. Walking into the Properties column's fields is what this
// used to do, and it made `tab` mean two different things on one press — move to
// the next column, or move to the next field — decided by where you already
// were. Entering the column with `f` is how you get at the fields.
func (m model) moveFocus(delta int) model {
	next := ((m.column()+delta)%3 + 3) % 3
	// A column left is a column released: the chrome can never be holding the
	// keyboard for a column that is not focused.
	m.zoneEntered = false
	m.editor.Blur()
	switch next {
	case 0:
		m.focus = focusScenarios
	case 1:
		m.focus = focusFrame
	default:
		if m.focus < 0 {
			// Land on the field the column was last left on, or its first.
			m.focus = 0
		}
	}
	return m.clampFrame()
}

// moveField walks the fields INSIDE the Properties column, once it is entered.
func (m model) moveField(delta int) model {
	total := len(m.fields())
	if total == 0 {
		return m
	}
	at := clamp(m.focus, 0, total-1)
	m.focus = ((at+delta)%total + total) % total
	return m.syncEditor()
}

// syncEditor points the text input at the focused field, or blurs it.
func (m model) syncEditor() model {
	m.editor.Blur()
	if field, ok := m.focusedField(); ok && m.zoneEntered && field.kind != propChoice {
		value := field.display()
		if field.kind == propNumber && field.number == 0 && field.zeroLabel != "" {
			// Start empty rather than on the word: typing a digit should set the
			// number, not append to a label.
			value = ""
		}
		m.editor.SetValue(value)
		m.editor.CursorEnd()
		m.editor.Focus()
	}
	return m.clampFrame()
}

// ---- geometry --------------------------------------------------------------

// bodyRows is everything above the footer. The footer is MEASURED rather than
// assumed: it wraps on a narrow terminal, and a constant would push the frame off
// the bottom exactly when there is least room to spare.
func (m model) bodyRows() int {
	const blankSeparator = 1
	return maxInt(m.height-lipgloss.Height(m.footer())-blankSeparator, 6)
}

func (m model) rightWidth() int {
	return maxInt(m.width-componentsWidth-paneGap, minFrameWidth+scenarioWidth+propsWidth+8)
}

func (m model) headerBoxWidth() int {
	return maxInt(m.rightWidth()-frameCost(m.styles.Panel), 24)
}

func (m model) headerTextWidth() int {
	return maxInt(m.headerBoxWidth()-m.styles.Panel.GetHorizontalPadding(), 20)
}

func (m model) headerRows() int { return lipgloss.Height(m.headerBox()) }

func (m model) descriptionBlock() string {
	lines := strings.Split(wrapAt(m.currentEntry().desc, m.headerTextWidth()), "\n")
	if cap := maxInt(m.bodyRows()/4, 3); len(lines) > cap {
		lines = lines[:cap]
		lines[cap-1] = fitLine(lines[cap-1], maxInt(m.headerTextWidth()-1, 1)) + "…"
	}
	return strings.Join(lines, "\n")
}

func (m model) maxFrameWidth() int {
	return maxInt(m.frameContainerWidth()-frameBorders, minFrameWidth)
}

func (m model) maxFrameHeight() int {
	return maxInt(m.panelRows()-frameBorders-statusRows, minFrameHeight)
}

// ---- view ------------------------------------------------------------------

func (m model) View() string {
	body := lipgloss.JoinHorizontal(lipgloss.Top, m.componentsPanel(), gap(), m.rightColumn())
	return strings.Join([]string{body, "", m.footer()}, "\n")
}

func gap() string { return strings.Repeat(" ", paneGap) }

func (m model) rightColumn() string {
	row := lipgloss.JoinHorizontal(lipgloss.Top,
		m.scenarioPanel(), gap(), m.frameContainer(), gap(), m.propertiesPanel())
	return strings.Join([]string{m.headerBox(), row}, "\n")
}

// frameContainer holds the frame at a FIXED size so shrinking the frame does not
// drag the Properties column left to chase it.
//
// Without it the columns are packed against each other and the whole right-hand
// side slides every time you type a width — which makes comparing two sizes
// impossible, because everything else moved too. The frame sits top-left inside
// the container and the slack is left blank.
func (m model) frameContainer() string {
	width, rows := m.frameContainerWidth(), m.panelRows()
	out := make([]string, 0, rows)
	for _, line := range strings.Split(m.frameBox(), "\n") {
		out = append(out, fitLine(line, width))
	}
	blank := strings.Repeat(" ", width)
	for len(out) < rows {
		out = append(out, blank)
	}
	return strings.Join(out[:rows], "\n")
}

func (m model) headerBox() string {
	e := m.currentEntry()
	title := m.styles.kicker(e.name) + m.styles.Muted.Render("  ·  ") + m.styles.Accent.Render(e.title)
	body := strings.Join([]string{
		fitLine(title, m.headerTextWidth()),
		m.styles.Muted.Render(m.descriptionBlock()),
	}, "\n")
	return m.styles.Panel.Width(m.headerBoxWidth()).Render(body)
}

// panelRows is the height of everything under the header — the scenario column,
// the frame's container and the Properties column all stand the same height, and
// that height is the SCREEN's, not the simulated frame's.
//
// Sizing them off the frame left a dead band under each one whenever the frame
// was shrunk, which is most of the time here: the tool is for looking at small
// geometries.
func (m model) panelRows() int {
	return maxInt(m.bodyRows()-m.headerRows(), 4)
}

func (m model) frameContainerWidth() int {
	return maxInt(m.rightWidth()-scenarioWidth-propsWidth-2*paneGap, minFrameWidth+frameBorders)
}

func (m model) componentsPanel() string {
	inner := maxInt(componentsWidth-frameCost(m.styles.Panel)-m.styles.Panel.GetHorizontalPadding(), 8)
	cardWidth := maxInt(inner-frameCost(m.styles.Card), 6)
	rows := make([]string, 0, len(m.entries))
	for i, e := range m.entries {
		style := m.styles.Card
		label := m.styles.Muted.Render(e.name)
		if i == m.sel {
			style = m.styles.CardOn
			label = m.styles.Accent.Render(e.name)
			if m.focus != focusComponents {
				label = m.styles.Text.Render(e.name)
			}
		}
		rows = append(rows, style.Width(cardWidth).Render(fitLine(label, maxInt(cardWidth-2, 3))))
	}
	body := strings.Join(rows, "\n")
	height := m.bodyRows() - m.styles.Panel.GetVerticalFrameSize()
	vp := bubbleviewport.New(inner, maxInt(height-2, 1))
	vp.SetContent(body)
	vp.SetYOffset(m.followOffset(rows, m.sel, maxInt(height-2, 1)))
	head := m.styles.kicker("components")
	if m.screenMode {
		head = m.styles.kicker("screens")
	}
	if m.focus == focusComponents {
		if m.screenMode {
			head = m.styles.kickerOn("screens")
		} else {
			head = m.styles.kickerOn("components")
		}
	}
	style := m.styles.Panel
	if m.focus == focusComponents {
		style = style.BorderForeground(m.styles.Accent.GetForeground())
	}
	boxWidth := maxInt(componentsWidth-frameCost(m.styles.Panel), 10)
	return style.Width(boxWidth).Render(strings.Join([]string{fitLine(head, inner), "", vp.View()}, "\n"))
}

// followOffset keeps the selected block inside the window. It is the gallery's
// own two lines of scroll arithmetic — borrowing scrollwindow would tie the tool
// to a package it exists to inspect.
func (m model) followOffset(blocks []string, selected, window int) int {
	top := 0
	for i := 0; i < selected && i < len(blocks); i++ {
		top += lipgloss.Height(blocks[i])
	}
	height := 1
	if selected >= 0 && selected < len(blocks) {
		height = lipgloss.Height(blocks[selected])
	}
	total := 0
	for _, b := range blocks {
		total += lipgloss.Height(b)
	}
	offset := 0
	if bottom := top + height; bottom > window {
		offset = bottom - window
	}
	return clamp(offset, 0, maxInt(total-window, 0))
}

func (m model) scenarioPanel() string {
	inner := maxInt(scenarioWidth-frameCost(m.styles.Panel)-m.styles.Panel.GetHorizontalPadding(), 8)
	rows := make([]string, 0, len(m.scenarios()))
	for i, s := range m.scenarios() {
		style := m.styles.Card
		label := m.styles.Muted.Render(s.label())
		if i == m.scenario {
			style = m.styles.CardOn
			label = m.styles.Accent.Render(s.label())
			if m.focus != focusScenarios {
				label = m.styles.Text.Render(s.label())
			}
		}
		cardWidth := maxInt(inner-frameCost(m.styles.Card), 6)
		rows = append(rows, style.Width(cardWidth).Render(wrapAt(label, maxInt(cardWidth-2, 3))))
	}
	body := strings.Join(rows, "\n")
	window := maxInt(m.panelRows()-m.styles.Panel.GetVerticalFrameSize()-2, 1)
	vp := bubbleviewport.New(inner, window)
	vp.SetContent(body)
	vp.SetYOffset(m.followOffset(rows, m.scenario, window))
	head := m.styles.kicker("scenario")
	if m.focus == focusScenarios {
		head = m.styles.kickerOn("scenario")
	}
	style := m.styles.Panel
	if m.focus == focusScenarios {
		style = style.BorderForeground(m.styles.Accent.GetForeground())
	}
	boxWidth := maxInt(scenarioWidth-frameCost(m.styles.Panel), 10)
	return style.Width(boxWidth).Render(strings.Join([]string{fitLine(head, inner), "", vp.View()}, "\n"))
}

func (m model) propertiesPanel() string {
	inner := maxInt(propsWidth-frameCost(m.styles.Panel)-m.styles.Panel.GetHorizontalPadding(), 10)
	blocks, offsets := m.propertyBlocks(inner)
	window := maxInt(m.panelRows()-m.styles.Panel.GetVerticalFrameSize()-2, 1)

	vp := bubbleviewport.New(inner, window)
	vp.SetContent(strings.Join(blocks, "\n"))
	if m.focus >= 0 && m.focus < len(offsets) {
		vp.SetYOffset(clamp(offsets[m.focus]-window/3, 0, maxInt(lipgloss.Height(strings.Join(blocks, "\n"))-window, 0)))
	}
	head := m.styles.kicker("properties")
	if m.focus >= 0 {
		head = m.styles.kickerOn("properties")
	}
	style := m.styles.Panel
	if m.focus >= 0 {
		style = style.BorderForeground(m.styles.Accent.GetForeground())
	}
	boxWidth := maxInt(propsWidth-frameCost(m.styles.Panel), 10)
	return style.Width(boxWidth).Render(strings.Join([]string{fitLine(head, inner), "", vp.View()}, "\n"))
}

// propertyBlocks renders each field and reports the line each one starts on, so
// the panel can scroll the focused field into view without re-deriving where it
// put it.
func (m model) propertyBlocks(inner int) ([]string, []int) {
	fields := m.fields()
	blocks := make([]string, 0, len(fields)+1)
	offsets := make([]int, len(fields))
	line := 0
	for i, field := range fields {
		if i == m.frameFieldCount() {
			divider := m.styles.rule(inner) + "\n" + fitLine(m.styles.kicker("component properties"), inner) + "\n"
			blocks = append(blocks, divider)
			line += lipgloss.Height(divider)
		}
		offsets[i] = line
		block := m.fieldBlock(field, i == m.focus, inner)
		blocks = append(blocks, block)
		line += lipgloss.Height(block)
	}
	return blocks, offsets
}

func (m model) fieldBlock(field prop, focused bool, inner int) string {
	label := m.styles.kicker(field.name)
	if focused {
		label = m.styles.kickerOn(field.name)
	}
	rows := []string{fitLine(label, inner)}
	switch field.kind {
	case propChoice:
		rows = append(rows, m.radio(field, focused, inner)...)
	default:
		rows = append(rows, m.valueBox(field, focused, inner))
	}
	// A value the pane cannot honour has to say so. A box reading 999 over a
	// frame drawn at 30 would make every observation taken here suspect.
	if focused && field.kind != propChoice {
		if typed := strings.TrimSpace(m.editor.Value()); typed != "" && typed != field.display() {
			rows = append(rows, m.styles.Warning.Render(fitLine(typed+" → "+field.display(), inner)))
		}
	}
	if focused && field.note != "" {
		rows = append(rows, m.styles.Muted.Render(wrapAt(field.note, inner)))
	}
	return strings.Join(append(rows, ""), "\n")
}

func (m model) valueBox(field prop, focused bool, inner int) string {
	box := lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(m.styles.Border.GetForeground())
	value := field.display()
	if focused {
		box = box.BorderForeground(m.styles.Accent.GetForeground())
		value = m.editor.View()
	}
	return box.Width(maxInt(inner-frameCost(box), 4)).Render(value)
}

// radio paints the options as filled and hollow dots, wrapping when the column is
// too narrow to hold them on one row.
func (m model) radio(field prop, focused bool, width int) []string {
	var rows []string
	var current string
	for i, option := range field.options {
		glyph, style := "○ ", m.styles.Muted
		if i == field.selected {
			glyph, style = "● ", m.styles.Text
			if focused {
				style = m.styles.Accent
			}
		}
		piece := style.Render(glyph + option)
		switch {
		case current == "":
			current = piece
		case lipgloss.Width(current)+2+lipgloss.Width(piece) <= width:
			current += "  " + piece
		default:
			rows = append(rows, fitLine(current, width))
			current = piece
		}
	}
	if current != "" {
		rows = append(rows, fitLine(current, width))
	}
	return rows
}

func (m model) frameBox() string {
	content, overflow := m.frameContent()
	border := m.styles.Border.GetForeground()
	style := lipgloss.NewStyle().Border(lipgloss.NormalBorder())
	if m.focus == focusFrame {
		border = m.styles.Accent.GetForeground()
	}
	if m.zoneEntered {
		// A double border for the one state in which a keystroke does something
		// other than what the rest of the tool does. Whether the chrome or the
		// component owns `tab` has to be readable without pressing it.
		style = style.Border(lipgloss.DoubleBorder())
	}
	box := style.BorderForeground(border).Render(content)
	return strings.Join([]string{box, m.statusLine(overflow)}, "\n")
}

// frameContent renders the component at the simulated size, then places it inside
// the frame at the chosen alignment and padding. Content that does not fit is
// CLIPPED and counted rather than allowed to push the frame open: a component
// that overflows its terminal is the thing worth seeing.
func (m model) frameContent() (string, int) {
	innerW, innerH := m.innerWidth(), m.innerHeight()
	lines := strings.Split(m.live.View(m.ctx()), "\n")
	overflow := 0
	if len(lines) > innerH {
		overflow = len(lines) - innerH
		lines = lines[:innerH]
	}
	vertical := align(propOption(m.frameProps, "align ↕"))
	horizontal := align(propOption(m.frameProps, "align ↔"))
	for i, line := range lines {
		if lipgloss.Width(line) > innerW {
			lines[i] = fitLine(line, innerW)
			continue
		}
		lines[i] = alignHorizontally(line, innerW, horizontal)
	}
	lines = alignVertically(lines, innerH, innerW, vertical)

	pad := strings.Repeat(" ", m.padding())
	blank := strings.Repeat(" ", m.frameW())
	out := make([]string, 0, m.frameH())
	for i := 0; i < m.padding(); i++ {
		out = append(out, blank)
	}
	for _, line := range lines {
		out = append(out, pad+line+pad)
	}
	for len(out) < m.frameH() {
		out = append(out, blank)
	}
	return strings.Join(out[:m.frameH()], "\n"), overflow
}

type align int

const (
	alignStart align = iota
	alignMiddle
	alignEnd
)

func alignHorizontally(line string, width int, a align) string {
	slack := width - lipgloss.Width(line)
	if slack <= 0 {
		return line
	}
	switch a {
	case alignMiddle:
		left := slack / 2
		return strings.Repeat(" ", left) + line + strings.Repeat(" ", slack-left)
	case alignEnd:
		return strings.Repeat(" ", slack) + line
	default:
		return line + strings.Repeat(" ", slack)
	}
}

func alignVertically(lines []string, rows, width int, a align) []string {
	slack := rows - len(lines)
	if slack <= 0 {
		return lines
	}
	blank := strings.Repeat(" ", width)
	above := 0
	switch a {
	case alignMiddle:
		above = slack / 2
	case alignEnd:
		above = slack
	}
	out := make([]string, 0, rows)
	for i := 0; i < above; i++ {
		out = append(out, blank)
	}
	out = append(out, lines...)
	for len(out) < rows {
		out = append(out, blank)
	}
	return out
}

func (m model) statusLine(overflow int) string {
	geometry := fmt.Sprintf("%d×%d", m.innerWidth(), m.innerHeight())
	if m.padding() > 0 {
		geometry = fmt.Sprintf("%d×%d −%d pad → %s", m.frameW(), m.frameH(), m.padding(), geometry)
	}
	parts := []string{geometry}
	if status := m.live.Status(m.ctx()); status != "" {
		parts = append(parts, status)
	}
	// The scenario's own summary is not repeated here: the status row is only as
	// wide as the frame, and it would push the component's read-out off the end.
	text := strings.Join(parts, " · ")
	// The read-out gets the CONTAINER's width, not the frame's: a narrow frame
	// leaves the room beside it empty anyway, and clipping the component's own
	// state to a 24-column frame throws away the thing you shrank it to read.
	width := m.frameContainerWidth()
	if overflow > 0 {
		text = fmt.Sprintf("⚠ +%d rows clipped · %s", overflow, text)
		return m.styles.Warning.Render(fitLine(text, width))
	}
	return m.styles.Muted.Render(fitLine(text, width))
}

// ---- footer ----------------------------------------------------------------

func (m model) footer() string {
	helper := m.help
	helper.Width = m.width
	// help.Model treats Width as a hint and hands back a row wider than it when
	// the bindings do not fit — which shears the whole layout on the one row the
	// tool uses to tell you what your keys do. The clip is ours because the
	// guarantee is ours: nothing this tool paints is wider than the terminal.
	return clipLine(helper.ShortHelpView(m.bindings()), m.width)
}

func (m model) bindings() []key.Binding {
	switch m.focus {
	case focusComponents:
		return []key.Binding{
			binding("j/k", "preview"),
			binding("m", "screens/components"),
			binding("enter", "take the keys"),
			binding("q", "quit"),
		}
	case focusScenarios:
		return []key.Binding{binding("↑/↓", "scenario"), binding("tab", "frame"), binding("esc", "back")}
	case focusFrame:
		if m.zoneEntered {
			// The component's own table, first and whole — once entered it is the
			// only table in effect, so nothing of the chrome's belongs in front
			// of it.
			return append(m.live.Help(), binding("esc", "leave"))
		}
		return append([]key.Binding{binding("f", "enter"), binding("tab", "properties")},
			append(m.live.Help(), binding("esc", "back"))...)
	}
	field, _ := m.focusedField()
	if !m.zoneEntered {
		return []key.Binding{
			binding("↑/↓", "field"), binding("f", "enter"),
			binding("tab", "scenario"), binding("esc", "back"),
		}
	}
	edit := binding("type", "set "+field.name)
	if field.kind == propChoice {
		edit = binding("←/→", "pick "+field.name)
	}
	return []key.Binding{binding("↑/↓ · tab", "field"), edit, binding("esc", "leave")}
}

// dumpAll renders every component at a fixed size in each of its scenarios. It is
// the non-interactive path: no TTY, so it pipes to a file and diffs.
func dumpAll(theme config.Theme, demos demoTheme, width, height int) string {
	var out strings.Builder
	chrome := newChromeStyles(theme)
	for _, e := range entries() {
		out.WriteString(chrome.kicker(e.name) + "  " + chrome.Accent.Render(e.title) + "\n")
		out.WriteString(chrome.Muted.Render(e.subject()) + "\n")
		out.WriteString(chrome.rule(width) + "\n")
		for _, s := range e.scenarios {
			ctx := demos.ctx(dumpDim(s.width, width), dumpDim(s.height, minInt(height, 16)))
			d := e.new(ctx)
			for _, p := range seed(d.Props(), s.props) {
				d = d.SetProp(p, ctx)
			}
			out.WriteString("\n" + chrome.Accent.Render("· "+s.name) + "  " + chrome.Muted.Render(s.summary()) + "\n")
			out.WriteString(chrome.Muted.Render("  "+d.Status(ctx)) + "\n")
			out.WriteString(d.View(ctx) + "\n")
		}
		out.WriteString("\n")
	}
	return out.String()
}

func dumpDim(scenario, fallback int) int {
	if scenario > 0 {
		return scenario
	}
	return fallback
}
