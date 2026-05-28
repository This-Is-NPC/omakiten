package palette

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func runCmd(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

func TestModelDefaultsToTricksTab(t *testing.T) {
	m := NewModel()
	if m.ActiveTab() != TabTricks {
		t.Fatalf("New().ActiveTab() = %v, want TabTricks", m.ActiveTab())
	}
}

func TestModelTabTogglesBetweenTabs(t *testing.T) {
	m := NewModel()
	m, _ = m.Update(keyMsg("tab"))
	if m.ActiveTab() != TabSearch {
		t.Fatalf("after tab, ActiveTab() = %v, want TabSearch", m.ActiveTab())
	}
	m, _ = m.Update(keyMsg("tab"))
	if m.ActiveTab() != TabTricks {
		t.Fatalf("after second tab, ActiveTab() = %v, want TabTricks", m.ActiveTab())
	}
}

func TestModelEscEmitsDismiss(t *testing.T) {
	m := NewModel()
	_, cmd := m.Update(keyMsg("esc"))
	msg := runCmd(cmd)
	if _, ok := msg.(DismissMsg); !ok {
		t.Fatalf("esc cmd msg = %T, want DismissMsg", msg)
	}
}

func TestModelEnterOnTricksParsesAndSubmits(t *testing.T) {
	m := NewModel()
	for _, r := range "nav:31" {
		m, _ = m.Update(keyMsg(string(r)))
	}
	_, cmd := m.Update(keyMsg("enter"))
	msg := runCmd(cmd)
	submit, ok := msg.(SubmitMsg)
	if !ok {
		t.Fatalf("enter cmd msg = %T, want SubmitMsg", msg)
	}
	if submit.Token.Verb != "nav" || submit.Token.Operand != "31" {
		t.Fatalf("SubmitMsg.Token = %+v, want {nav 31 nav:31}", submit.Token)
	}
}

func TestModelEnterOnTricksParseErrorShowsStatus(t *testing.T) {
	m := NewModel()
	for _, r := range "nav31" {
		m, _ = m.Update(keyMsg(string(r)))
	}
	m, cmd := m.Update(keyMsg("enter"))
	if runCmd(cmd) != nil {
		t.Fatalf("parse error cmd should be nil, got %v", runCmd(cmd))
	}
	if m.Status() == "" {
		t.Fatalf("parse error should set inline status, got empty")
	}
}

func TestModelEnterOnSearchSubmitsQuery(t *testing.T) {
	m := NewModel()
	m, _ = m.Update(keyMsg("tab"))
	for _, r := range "ports" {
		m, _ = m.Update(keyMsg(string(r)))
	}
	_, cmd := m.Update(keyMsg("enter"))
	msg := runCmd(cmd)
	search, ok := msg.(SearchMsg)
	if !ok {
		t.Fatalf("enter on search cmd msg = %T, want SearchMsg", msg)
	}
	if search.Query != "ports" {
		t.Fatalf("SearchMsg.Query = %q, want ports", search.Query)
	}
}

func TestModelEnterOnSearchEmptyShowsStatus(t *testing.T) {
	m := NewModel()
	m, _ = m.Update(keyMsg("tab"))
	m, cmd := m.Update(keyMsg("enter"))
	if runCmd(cmd) != nil {
		t.Fatalf("empty search cmd should be nil, got %v", runCmd(cmd))
	}
	if m.Status() == "" {
		t.Fatalf("empty search should set inline status, got empty")
	}
}

func TestModelTabPreservesPerTabInput(t *testing.T) {
	m := NewModel()
	for _, r := range "nav:31" {
		m, _ = m.Update(keyMsg(string(r)))
	}
	m, _ = m.Update(keyMsg("tab"))
	for _, r := range "ports" {
		m, _ = m.Update(keyMsg(string(r)))
	}
	m, _ = m.Update(keyMsg("tab"))
	if m.Tricks() != "nav:31" {
		t.Fatalf("Tricks tab content = %q, want nav:31 (preserved across tab toggle)", m.Tricks())
	}
	if m.Search() != "ports" {
		t.Fatalf("Search tab content = %q, want ports", m.Search())
	}
}

func TestModelTabClearsStatus(t *testing.T) {
	m := NewModel()
	for _, r := range "nav31" {
		m, _ = m.Update(keyMsg(string(r)))
	}
	m, _ = m.Update(keyMsg("enter"))
	if m.Status() == "" {
		t.Fatalf("setup: expected non-empty status after parse error")
	}
	m, _ = m.Update(keyMsg("tab"))
	if m.Status() != "" {
		t.Fatalf("after tab, Status() = %q, want empty", m.Status())
	}
}

func TestModelSetStatusFromOutside(t *testing.T) {
	m := NewModel()
	m.SetStatus("handler said no")
	if m.Status() != "handler said no" {
		t.Fatalf("Status() = %q, want %q", m.Status(), "handler said no")
	}
}
