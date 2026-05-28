package palette

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/domain"
)

// resultListMaxWidth caps each rendered result row at the inner
// content width of the parent's palette panel — the parent in
// internal/tui/model.go::renderPaletteOverlay wraps the overlay
// in a rounded-border lipgloss style with Width(48) and Padding(0,2),
// so 48 - 2*2 padding - 2 border = 44 cells of usable content.
// Keeping the budget here (rather than threading SetWidth from
// the parent on every open + resize) trades a tiny coupling for
// no plumbing churn; the constant is the single source the tests
// also lock against.
const resultListMaxWidth = 44

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
// tab with a non-empty query and no result list focused. The
// caller routes the query to SearchService and posts the hits
// back via SetResults.
type SearchMsg struct {
	Query string
}

// OpenHitMsg flows out when the user presses Enter on the Search
// tab while a result row is focused. The caller dispatches the
// hit to its TUI detail view (task / comment per #319 D1) or
// surfaces an inline hint when the entity type has no TUI screen.
type OpenHitMsg struct {
	Hit domain.SearchHit
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
	// results carries the most recent FTS5 hits the parent posted
	// via SetResults. Empty slice = no list rendered; non-empty =
	// the Search-tab View renders the navigable list and Enter on
	// a focused row emits OpenHitMsg instead of SearchMsg.
	results       []domain.SearchHit
	resultsCursor int
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

// Results returns a copy of the current hit list so callers can
// inspect or render without aliasing the model's slice.
func (m Model) Results() []domain.SearchHit {
	out := make([]domain.SearchHit, len(m.results))
	copy(out, m.results)
	return out
}

// ResultsCursor returns the index of the focused row. Zero when
// no results are loaded.
func (m Model) ResultsCursor() int { return m.resultsCursor }

// HasResults reports whether the Search tab currently has a
// navigable list to render. Used by the parent's View to widen
// the overlay panel and by Update to route up/down/pgup/pgdn keys.
func (m Model) HasResults() bool { return len(m.results) > 0 }

// FocusedHit returns the hit under the cursor, or (zero, false)
// when no results are present. Callers do not have to bounds-
// check resultsCursor — SetResults / navigation methods clamp it.
func (m Model) FocusedHit() (domain.SearchHit, bool) {
	if !m.HasResults() {
		return domain.SearchHit{}, false
	}
	return m.results[m.resultsCursor], true
}

// SetResults replaces the current result list and resets the
// cursor to the first row. Callers post this from the async
// search completion arm; the model owns the cursor invariants so
// the dispatch layer never has to clamp.
//
// Side effect: status is cleared. Setting fresh results implies
// the prior "searching <q>…" / "no results for <q>" hint is now
// stale, and showing it next to a populated list confuses the
// user. Documented here so the contract is explicit (#319 review
// finding I4).
//
// The hits slice is copied into the model so caller-side mutation
// after SetResults does not alias internal state (#319 review
// finding W1).
func (m *Model) SetResults(hits []domain.SearchHit) {
	m.results = append([]domain.SearchHit(nil), hits...)
	m.resultsCursor = 0
	m.status = ""
}

// ClearResults drops the result list and resets the cursor. Used
// by callers that want to recycle the overlay without re-creating
// it (e.g. opening a brand-new search session).
func (m *Model) ClearResults() {
	m.results = nil
	m.resultsCursor = 0
}

// Update routes the keypress and bubbles the appropriate outbound
// message (SubmitMsg, SearchMsg, OpenHitMsg, DismissMsg) up
// through tea.Cmd so the parent can react.
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
	case "up", "k":
		if m.tab == TabSearch && m.HasResults() {
			m.moveResultsCursor(-1)
			return m, nil
		}
	case "down", "j":
		if m.tab == TabSearch && m.HasResults() {
			m.moveResultsCursor(1)
			return m, nil
		}
	case "pgup":
		if m.tab == TabSearch && m.HasResults() {
			m.moveResultsCursor(-5)
			return m, nil
		}
	case "pgdown":
		if m.tab == TabSearch && m.HasResults() {
			m.moveResultsCursor(5)
			return m, nil
		}
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

func (m *Model) moveResultsCursor(delta int) {
	next := m.resultsCursor + delta
	if next < 0 {
		next = 0
	}
	if next >= len(m.results) {
		next = len(m.results) - 1
	}
	m.resultsCursor = next
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
	if m.HasResults() {
		hit, ok := m.FocusedHit()
		if !ok {
			return m, nil
		}
		m.status = ""
		return m, openHitCmd(hit)
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

// markTagPattern strips FTS5 snippet highlight markers. The
// adapter wraps query-matching tokens in <mark>…</mark>; the
// terminal renderer cannot honour HTML, so the tags would leak
// through as literal text. Stripping keeps the snippet readable
// without losing the underlying word.
var markTagPattern = regexp.MustCompile(`</?mark>`)

// View renders the overlay panel. Tricks tab keeps the prior
// shape (single input + status). Search tab renders the input
// row plus, when results are present, the navigable list with
// cursor marker and cleaned snippets.
func (m Model) View() string {
	tabs := "[ tricks ]  search"
	if m.tab == TabSearch {
		tabs = "tricks  [ search ]"
	}
	var body string
	if m.tab == TabTricks {
		body = m.tricks.View()
	} else {
		body = m.search.View()
		if m.HasResults() {
			body += "\n\n" + m.renderResultList()
		}
	}
	out := tabs + "\n" + body
	if m.status != "" {
		out += "\n" + m.status
	}
	return out
}

// renderResultList draws the navigable hit list. Header row is
// the result count; each row carries a cursor marker, the entity
// type, the id, and the cleaned snippet (mark tags stripped,
// newlines collapsed, ANSI escapes stripped, width-budgeted to
// fit the parent panel). No top-N cap — the user sees every hit
// the FTS5 adapter returned.
func (m Model) renderResultList() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d results", len(m.results))
	for i, hit := range m.results {
		marker := "  "
		if i == m.resultsCursor {
			marker = "▸ "
		}
		prefix := fmt.Sprintf("%s%s #%d  ", marker, hit.EntityType, hit.ID)
		snippet := cleanSnippet(hit.Snippet)
		budget := resultListMaxWidth - ansi.StringWidth(prefix)
		if budget < 1 {
			budget = 1
		}
		fmt.Fprintf(&b, "\n%s%s", prefix, ansi.Truncate(snippet, budget, "…"))
	}
	return b.String()
}

// cleanSnippet sanitises the FTS5 snippet for terminal rendering:
// strips <mark> highlight tags (they would print verbatim as HTML
// text), collapses embedded newlines so each hit fits one visual
// row, and removes ANSI escape sequences carried in upstream task
// titles / comment bodies (intentional or hostile — protects the
// terminal from injection per #319 review finding W3).
func cleanSnippet(snippet string) string {
	out := markTagPattern.ReplaceAllString(snippet, "")
	out = strings.ReplaceAll(out, "\n", " ")
	out = strings.ReplaceAll(out, "\r", " ")
	out = ansi.Strip(out)
	return strings.TrimSpace(out)
}

func toggle(t Tab) Tab {
	if t == TabTricks {
		return TabSearch
	}
	return TabTricks
}

func dismissCmd() tea.Cmd { return func() tea.Msg { return DismissMsg{} } }
func submitCmd(t Token) tea.Cmd {
	return func() tea.Msg { return SubmitMsg{Token: t} }
}
func searchCmd(q string) tea.Cmd {
	return func() tea.Msg { return SearchMsg{Query: q} }
}
func openHitCmd(h domain.SearchHit) tea.Cmd {
	return func() tea.Msg { return OpenHitMsg{Hit: h} }
}

// parseStatusMessage maps a Parse error sentinel onto a stable
// human-facing string. Caller-side i18n substitution will swap
// these for translated forms (deferred follow-up) — keeping the
// messages here for now lets the tests stay self-contained.
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
