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

// Column widths for the `| id | type | result |` table layout
// rendered inside the overlay panel. The marker cell already
// carries its own trailing space ("▸ " / "  "), so only the
// id↔type and type↔result column boundaries need a one-cell gap.
// The sum must equal resultListMaxWidth so the horizontal `─`
// rule lines up beneath the header row without overrun.
const (
	colWidthMarker = 2
	colWidthID     = 6
	colWidthType   = 8
	colWidthResult = resultListMaxWidth - colWidthMarker - colWidthID - 1 - colWidthType - 1
)

// resultListPageStep is the cursor delta applied by pgup/pgdown
// inside the navigable result list. Centralised so future tuning
// (e.g. tying it to terminal height) lands in one place instead
// of two parallel arms inside Update.
const resultListPageStep = 5

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
	// resultsScroll is the index of the first row inside the visible
	// window. It follows the cursor with an edge-anchored rule (same
	// shape as `syncFocusedColumnScroll` for the board's card lists):
	// the window only slides when the cursor would otherwise leave
	// the [scroll, scroll+maxResultRows) range. A centred window
	// would scroll on every down-arrow which feels jumpy and breaks
	// parity with the rest of the TUI's tables.
	resultsScroll int
	// maxResultRows caps the number of result rows renderResultList
	// emits per View() pass. Zero (the default) means unlimited —
	// callers that do not plumb terminal height keep the legacy
	// "render every hit" behaviour. The parent (renderPaletteOverlay)
	// computes a viewport budget from `tea.WindowSizeMsg` and calls
	// SetMaxResultRows so the overlay never grows taller than the
	// terminal, pushing chrome (borders, hints, base view) off-screen.
	maxResultRows int
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
	m.resultsScroll = 0
	m.status = ""
}

// ClearResults drops the result list and resets the cursor. Used
// by callers that want to recycle the overlay without re-creating
// it (e.g. opening a brand-new search session).
func (m *Model) ClearResults() {
	m.results = nil
	m.resultsCursor = 0
	m.resultsScroll = 0
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
		if m.resultsFocused() {
			m.moveResultsCursor(-1)
			return m, nil
		}
	case "down", "j":
		if m.resultsFocused() {
			m.moveResultsCursor(1)
			return m, nil
		}
	case "pgup":
		if m.resultsFocused() {
			m.moveResultsCursor(-resultListPageStep)
			return m, nil
		}
	case "pgdown":
		if m.resultsFocused() {
			m.moveResultsCursor(resultListPageStep)
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

// resultsFocused reports whether the navigable result list owns
// the keyboard. Centralises the gate the up/down/pgup/pgdown arms
// share — duplicated guard was flagged by #319 review finding I1.
func (m Model) resultsFocused() bool {
	return m.tab == TabSearch && m.HasResults()
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
	m.followResultsScroll()
}

// followResultsScroll keeps `resultsScroll` consistent with the
// cursor under an edge-anchored rule: only when the cursor would
// otherwise leave the visible [scroll, scroll+maxResultRows) range
// do we slide the window — same shape as `syncFocusedColumnScroll`
// for the board's card lists. The scroll is also clamped against
// the tail of the result slice so a shrinking window (e.g. after a
// terminal resize) does not leave dead rows below the list.
func (m *Model) followResultsScroll() {
	if m.maxResultRows <= 0 {
		m.resultsScroll = 0
		return
	}
	if m.resultsCursor < m.resultsScroll {
		m.resultsScroll = m.resultsCursor
	}
	if m.resultsCursor >= m.resultsScroll+m.maxResultRows {
		m.resultsScroll = m.resultsCursor - m.maxResultRows + 1
	}
	if m.resultsScroll < 0 {
		m.resultsScroll = 0
	}
	maxScroll := len(m.results) - m.maxResultRows
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.resultsScroll > maxScroll {
		m.resultsScroll = maxScroll
	}
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

// SetMaxResultRows sets the upper bound on result rows rendered by
// View(). Negative values are clamped to zero (= unlimited). The
// parent should compute the budget from the most recent terminal
// height so the overlay never overflows the viewport.
func (m *Model) SetMaxResultRows(n int) {
	if n < 0 {
		n = 0
	}
	m.maxResultRows = n
	m.followResultsScroll()
}

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
	var b strings.Builder
	if m.tab == TabSearch {
		b.WriteString("tricks  [ search ]\n")
	} else {
		b.WriteString("[ tricks ]  search\n")
	}
	if m.tab == TabTricks {
		b.WriteString(m.tricks.View())
	} else {
		b.WriteString(m.search.View())
		if m.HasResults() {
			b.WriteString("\n\n")
			b.WriteString(m.renderResultList())
		}
	}
	if m.status != "" {
		b.WriteByte('\n')
		b.WriteString(m.status)
	}
	return b.String()
}

// renderResultList draws the navigable hit list as a three-column
// table — id, type, result — matching the visual language used by
// `renderLogsWidePanel` in render_logs.go: count line, header row,
// horizontal rule, body rows. Every row carries a cursor marker
// in its leading cell; the result column holds the cleaned
// snippet (mark tags stripped, newlines collapsed, ANSI escapes
// stripped, width-budgeted to colWidthResult). When maxResultRows
// > 0 the list slides a fixed-size window around the cursor so
// the overlay never grows past the parent's viewport budget; an
// `↑N more` / `↓N more` indicator row makes the truncation
// visible.
func (m Model) renderResultList() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d results", len(m.results))
	fmt.Fprintf(&b, "\n%s%-*s %-*s %s",
		strings.Repeat(" ", colWidthMarker),
		colWidthID, "ID",
		colWidthType, "TYPE",
		"RESULT",
	)
	b.WriteByte('\n')
	b.WriteString(strings.Repeat("─", resultListMaxWidth))
	start, end := m.resultsWindow()
	if start > 0 {
		fmt.Fprintf(&b, "\n  ↑ %d more", start)
	}
	for i := start; i < end; i++ {
		marker := "  "
		if i == m.resultsCursor {
			marker = "▸ "
		}
		b.WriteByte('\n')
		b.WriteString(formatResultRow(marker, m.results[i]))
	}
	if end < len(m.results) {
		fmt.Fprintf(&b, "\n  ↓ %d more", len(m.results)-end)
	}
	return b.String()
}

// formatResultRow lays out one body row of the result table. The
// id and type columns are left-aligned to their fixed widths; the
// result column carries the snippet truncated to colWidthResult so
// the row width never exceeds resultListMaxWidth.
func formatResultRow(marker string, hit domain.SearchHit) string {
	id := fmt.Sprintf("#%d", hit.ID)
	snippet := ansi.Truncate(cleanSnippet(hit.Snippet), colWidthResult, "…")
	return fmt.Sprintf("%s%-*s %-*s %s",
		marker,
		colWidthID, id,
		colWidthType, string(hit.EntityType),
		snippet,
	)
}

// resultsWindow returns the [start, end) slice of `m.results` that
// renderResultList should emit. Zero maxResultRows (the default)
// disables windowing and renders every hit. Otherwise the window
// is anchored on `resultsScroll`, which `followResultsScroll`
// updates only when the cursor would otherwise leave the visible
// range — matching the edge-anchored scroll the rest of the TUI's
// tables use.
func (m Model) resultsWindow() (int, int) {
	n := len(m.results)
	if m.maxResultRows <= 0 || n <= m.maxResultRows {
		return 0, n
	}
	start := m.resultsScroll
	if start < 0 {
		start = 0
	}
	end := start + m.maxResultRows
	if end > n {
		end = n
	}
	return start, end
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
