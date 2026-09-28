package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures/runtimecache"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/plannetwork"
	"omakiten/internal/tui/screens/taskdetail"
)

func TestDeniedTUISurfaceHidesFooterAndHelp(t *testing.T) {
	t.Parallel()
	m := deniedDeleteTaskDetailModel(t)

	tokens := m.footerTokens()
	if footerHasFragment(tokens, "d") {
		t.Fatalf("task.delete tui:false must hide d from the footer; tokens=%v", tokens)
	}
	if !footerHasFragment(tokens, "x") {
		t.Fatalf("sibling archive key x must stay when only task.delete is denied; tokens=%v", tokens)
	}
	if !footerHasFragment(tokens, "?") {
		t.Fatalf("help key must stay; tokens=%v", tokens)
	}

	groups := m.screenHelpGroups(screenhost.TaskDetail)
	if helpHasFragment(groups, "d") {
		t.Fatalf("task.delete tui:false must hide d from help; groups=%v", groups)
	}
	if !helpHasFragment(groups, "x") {
		t.Fatalf("sibling archive key x must stay in help; groups=%v", groups)
	}
}

func TestDeniedTUISurfaceKeyShowsStatus(t *testing.T) {
	t.Parallel()
	m := deniedDeleteTaskDetailModel(t)
	if _, handled := m.dispatchOwnedScreenKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}}); !handled {
		t.Fatal("denied delete key must be consumed by the host")
	}
	if !strings.Contains(m.status, "delete hidden in tui") {
		t.Fatalf("status = %q, want the configured surfaces reason", m.status)
	}
}

func TestEmptySurfaceTableKeepsDeleteFooter(t *testing.T) {
	t.Parallel()
	m := newFooterFidelityModel(t)
	m.taskDetailScreen = taskdetail.New().Open(taskdetail.Payload{
		Task: domain.Task{ID: 1},
	}, m.screenFrame())
	m.screenStack = []screenhost.ID{screenhost.TaskDetail}

	tokens := m.footerTokens()
	if !footerHasFragment(tokens, "d") {
		t.Fatalf("empty surfaces table must keep d; tokens=%v", tokens)
	}
}

func TestDeniedTUISurfaceHidesBlockersKey(t *testing.T) {
	t.Parallel()
	m := deniedBlockersTaskDetailModel(t)

	tokens := m.footerTokens()
	if footerHasFragment(tokens, "b") {
		t.Fatalf("dependency.add tui:false must hide b from the footer; tokens=%v", tokens)
	}
	if !footerHasFragment(tokens, "c") {
		t.Fatalf("sibling comment key c must stay; tokens=%v", tokens)
	}

	groups := m.screenHelpGroups(screenhost.TaskDetail)
	if helpHasFragment(groups, "b") {
		t.Fatalf("dependency.add tui:false must hide b from help; groups=%v", groups)
	}
}

func TestDeniedTUISurfaceBlockersKeyShowsStatus(t *testing.T) {
	t.Parallel()
	m := deniedBlockersTaskDetailModel(t)
	if _, handled := m.dispatchOwnedScreenKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}}); !handled {
		t.Fatal("denied blockers key must be consumed by the host")
	}
	if !strings.Contains(m.status, "blockers hidden in tui") {
		t.Fatalf("status = %q, want the configured surfaces reason", m.status)
	}
}

func TestDeniedTUISurfaceHidesAssigneeKey(t *testing.T) {
	t.Parallel()
	m := deniedAssigneePlanNetworkModel(t)

	tokens := m.footerTokens()
	if footerHasFragment(tokens, "c") {
		t.Fatalf("task.assign tui:false must hide c from the footer; tokens=%v", tokens)
	}
	if !footerHasFragment(tokens, "e") {
		t.Fatalf("sibling edit-goal key e must stay; tokens=%v", tokens)
	}

	groups := m.screenHelpGroups(screenhost.PlanNetwork)
	if helpHasFragment(groups, "c") {
		t.Fatalf("task.assign tui:false must hide c from help; groups=%v", groups)
	}
	if !helpHasFragment(groups, "e") {
		t.Fatalf("sibling edit-goal key e must stay in help; groups=%v", groups)
	}
}

func deniedDeleteTaskDetailModel(t *testing.T) *Model {
	t.Helper()
	table := config.CanonicalSurfaceTable()
	row := table["task.delete"]
	off := false
	row.TUI = &off
	row.Reason = "delete hidden in tui"
	table["task.delete"] = row
	snap := config.BuildSnapshot(config.Bundle{Surfaces: table})

	m := newFooterFidelityModel(t)
	m.repos.Cache = runtimecache.Install(0, snap)
	m.repos.ProjectID = 0
	m.taskDetailScreen = taskdetail.New().Open(taskdetail.Payload{
		Task: domain.Task{ID: 1},
	}, m.screenFrame()).WithFocus(taskdetail.FocusDetails)
	m.screenStack = []screenhost.ID{screenhost.TaskDetail}
	return &m
}

func deniedBlockersTaskDetailModel(t *testing.T) *Model {
	t.Helper()
	table := config.CanonicalSurfaceTable()
	row := table["dependency.add"]
	off := false
	row.TUI = &off
	row.Reason = "blockers hidden in tui"
	table["dependency.add"] = row
	snap := config.BuildSnapshot(config.Bundle{Surfaces: table})

	m := newFooterFidelityModel(t)
	m.repos.Cache = runtimecache.Install(0, snap)
	m.repos.ProjectID = 0
	m.taskDetailScreen = taskdetail.New().Open(taskdetail.Payload{
		Task: domain.Task{ID: 1},
	}, m.screenFrame())
	m.screenStack = []screenhost.ID{screenhost.TaskDetail}
	return &m
}

func deniedAssigneePlanNetworkModel(t *testing.T) *Model {
	t.Helper()
	table := config.CanonicalSurfaceTable()
	row := table["task.assign"]
	off := false
	row.TUI = &off
	row.Reason = "assignee hidden in tui"
	table["task.assign"] = row
	snap := config.BuildSnapshot(config.Bundle{Surfaces: table})

	m := newFooterFidelityModel(t)
	m.repos.Cache = runtimecache.Install(0, snap)
	m.repos.ProjectID = 0
	m.planNetworkScreen = plannetwork.New().Open(plannetwork.Payload{
		Show: domain.PlanShow{Plan: domain.Plan{ID: 1, Slug: "p1"}},
	})
	m.screenStack = []screenhost.ID{screenhost.PlanNetwork}
	return &m
}

func helpHasFragment(groups []screenhost.HelpGroup, fragment string) bool {
	for _, group := range groups {
		for _, binding := range group.Bindings {
			for _, frag := range bindingKeyFragments(binding.Key) {
				if frag == fragment {
					return true
				}
			}
		}
	}
	return false
}

func footerHasFragment(tokens []footerToken, fragment string) bool {
	for _, tok := range tokens {
		for _, frag := range splitKeyToken(tok.key) {
			if frag == fragment {
				return true
			}
		}
	}
	return false
}

func newFooterFidelityModel(t *testing.T) Model {
	t.Helper()
	return Model{
		styles:    newStyles(config.Theme{}),
		width:     160,
		height:    40,
		languages: config.LanguageSettings{CLI: "en", TUI: "en"},
		repos:     Repositories{Catalog: newTestCatalog(t)},
	}
}
