package palette

import (
	"errors"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Tab identifies which input the palette overlay is currently
// driving. Tricks is the default landing surface (per AC 1); the
// Search tab wraps the existing FTS5 SearchService and is reached
// via Tab from the Tricks tab.
type Tab int

const (
	TabTricks Tab = iota
	TabSearch
)

// SubmitMsg flows out of the overlay when the user presses Enter
// on the Tricks tab with a parseable input. Callers (the TUI root
// Update) consume it to dispatch the trick through the built-in
// handler.
type SubmitMsg struct {
	Token Token
}

// SearchMsg flows out when the user presses Enter on the Search
// tab. Query is the raw textinput value; the caller routes it to
// SearchService.
type SearchMsg struct {
	Query string
}

// DismissMsg flows out when the user presses Esc. The parent
// closes the overlay and restores the prior screen state (AC 9).
type DismissMsg struct{}

// Model is the Bubbletea overlay. Both textinputs persist across
// tab toggles so a partial query on Search is not lost when the
// user flips back to Tricks to fix a code.
type Model struct {
	tab    Tab
	tricks textinput.Model
	search textinput.Model
	// status carries the most recent inline error (parse failure)
	// or hint. Empty means no inline message; the View suppresses
	// the row entirely so the overlay stays compact.
	status string
}

// NewModel constructs a palette overlay with focus on the Tricks
// tab. Both textinputs start empty; the caller is responsible for
// re-creating the Model on each open so prior input does not leak
// across sessions.
func NewModel() Model {
	tricks := textinput.New()
	tricks.Prompt = ""
	tricks.CharLimit = 0
	tricks.Placeholder = "verb:operand"
	tricks.Focus()

	search := textinput.New()
	search.Prompt = ""
	search.CharLimit = 0
	search.Placeholder = "search…"
	search.Blur()

	return Model{
		tab:    TabTricks,
		tricks: tricks,
		search: search,
	}
}

// Tab returns the active tab. Exposed for the parent's View pass
// so chrome (kicker, hint) renders the correct labels.
func (m Model) ActiveTab() Tab { return m.tab }

// Status returns the most recent inline status string. Empty
// when no inline message is in effect.
func (m Model) Status() string { return m.status }

// Tricks / Search return the current textinput values so tests can
// assert content without reaching into private state.
func (m Model) Tricks() string { return m.tricks.Value() }
func (m Model) Search() string { return m.search.Value() }

// Update routes the keypress and bubbles the appropriate outbound
// message (SubmitMsg, SearchMsg, DismissMsg) up through tea.Cmd so
// the parent can react. Returns (Model, tea.Cmd) to fit the
// Bubbletea Update contract even though many paths return nil cmd.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		if m.tab == TabTricks {
			m.tricks, cmd = m.tricks.Update(msg)
		} else {
			m.search, cmd = m.search.Update(msg)
		}
		return m, cmd
	}
	switch key.String() {
	case "esc":
		return m, dismissCmd()
	case "tab":
		m.tab = toggle(m.tab)
		m.status = ""
		if m.tab == TabTricks {
			m.tricks.Focus()
			m.search.Blur()
		} else {
			m.search.Focus()
			m.tricks.Blur()
		}
		return m, nil
	case "enter":
		return m.submit()
	}
	var cmd tea.Cmd
	if m.tab == TabTricks {
		m.tricks, cmd = m.tricks.Update(msg)
	} else {
		m.search, cmd = m.search.Update(msg)
	}
	m.status = ""
	return m, cmd
}

func (m Model) submit() (Model, tea.Cmd) {
	if m.tab == TabTricks {
		token, err := Parse(m.tricks.Value())
		if err != nil {
			m.status = parseStatusMessage(err)
			return m, nil
		}
		m.status = ""
		return m, submitCmd(token)
	}
	query := m.search.Value()
	if query == "" {
		m.status = "search query is empty"
		return m, nil
	}
	m.status = ""
	return m, searchCmd(query)
}

// SetStatus lets the parent push a runtime status (handler error,
// dispatch result) back into the overlay so the user sees the
// outcome inside the panel rather than via the global status row.
func (m *Model) SetStatus(s string) { m.status = s }

// View renders the overlay panel. Tabs render as a header strip,
// the active textinput's View() owns its own row, and any inline
// status appears beneath. Styling is deliberately plain — the
// parent wraps the result in chrome (border, kicker) matching the
// surrounding theme; centralising layout there means a future
// theme rotation does not have to touch this package.
func (m Model) View() string {
	tabs := "[ tricks ]  search"
	if m.tab == TabSearch {
		tabs = "tricks  [ search ]"
	}
	var input string
	if m.tab == TabTricks {
		input = m.tricks.View()
	} else {
		input = m.search.View()
	}
	out := tabs + "\n" + input
	if m.status != "" {
		out += "\n" + m.status
	}
	return out
}

func toggle(t Tab) Tab {
	if t == TabTricks {
		return TabSearch
	}
	return TabTricks
}

func dismissCmd() tea.Cmd  { return func() tea.Msg { return DismissMsg{} } }
func submitCmd(t Token) tea.Cmd {
	return func() tea.Msg { return SubmitMsg{Token: t} }
}
func searchCmd(q string) tea.Cmd {
	return func() tea.Msg { return SearchMsg{Query: q} }
}

// parseStatusMessage maps a Parse error sentinel onto a stable
// human-facing string. Caller-side i18n substitution will swap
// these for translated forms (increment 7) — keeping the messages
// here for now lets the increment-4 tests stay self-contained.
func parseStatusMessage(err error) string {
	switch {
	case errors.Is(err, ErrMissingColon):
		return "missing : separator"
	case errors.Is(err, ErrTooManyColons):
		return "too many : separators"
	case errors.Is(err, ErrEmptyVerb):
		return "verb is empty"
	case errors.Is(err, ErrEmptyOperand):
		return "operand is empty"
	case errors.Is(err, ErrInvalidVerb):
		return "verb must match [a-z][a-z0-9_-]*"
	default:
		return err.Error()
	}
}
