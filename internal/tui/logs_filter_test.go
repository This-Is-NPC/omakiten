package tui

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/domain"
)

// TestLogsFilterCycleForwardOrder pins the umbrella's AC #2 chip
// rotation: `all → tool-calls → domain → system → all`. The test
// walks the cycle helper once per step and asserts the sequence
// returned matches the documented order — failure means a chip drift
// has landed in logs_filter.go and the help-row description in
// en.yaml + .docs/tui.md is out of sync with the runtime.
func TestLogsFilterCycleForwardOrder(t *testing.T) {
	want := []LogsFilterMode{
		LogsFilterAll,
		LogsFilterToolCalls,
		LogsFilterDomain,
		LogsFilterSystem,
		LogsFilterAll, // rollover
	}
	got := []LogsFilterMode{LogsFilterAll}
	mode := LogsFilterAll
	for i := 0; i < len(want)-1; i++ {
		mode = logsFilterCycle(mode, 1)
		got = append(got, mode)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("forward cycle = %v, want %v", got, want)
	}
}

// TestLogsFilterCycleBackwardOrder pins the reverse rotation triggered
// by shift+F. Walking backward from `all` must land on `system` first
// (umbrella AC #2 covers forward; reverse is the inverse and the
// scope description in the task also calls it out explicitly).
func TestLogsFilterCycleBackwardOrder(t *testing.T) {
	want := []LogsFilterMode{
		LogsFilterAll,
		LogsFilterSystem,
		LogsFilterDomain,
		LogsFilterToolCalls,
		LogsFilterAll, // rollover
	}
	got := []LogsFilterMode{LogsFilterAll}
	mode := LogsFilterAll
	for i := 0; i < len(want)-1; i++ {
		mode = logsFilterCycle(mode, -1)
		got = append(got, mode)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("backward cycle = %v, want %v", got, want)
	}
}

// TestLogsFilterCategoriesMapping pins the mode → repository-filter
// projection. The slice equality is order-insensitive on purpose:
// EventFilter.Categories is consumed as a set (SQL IN clause) so the
// invariant is membership, not order. Failure here means the panel
// rows the user sees will no longer match the chip label.
func TestLogsFilterCategoriesMapping(t *testing.T) {
	cases := []struct {
		name string
		mode LogsFilterMode
		want []domain.EventCategory
	}{
		{
			name: "all → nil (no filter)",
			mode: LogsFilterAll,
			want: nil,
		},
		{
			name: "tool-calls → tool_call + hook",
			mode: LogsFilterToolCalls,
			want: []domain.EventCategory{
				domain.EventCategoryToolCall,
				domain.EventCategoryHook,
			},
		},
		{
			name: "domain → task / comment / plan / trick / tag-dep",
			mode: LogsFilterDomain,
			want: []domain.EventCategory{
				domain.EventCategoryTask,
				domain.EventCategoryComment,
				domain.EventCategoryPlan,
				domain.EventCategoryTrick,
				domain.EventCategoryTagDep,
			},
		},
		{
			name: "system → audit / guard / domain",
			mode: LogsFilterSystem,
			want: []domain.EventCategory{
				domain.EventCategoryAudit,
				domain.EventCategoryGuard,
				domain.EventCategoryDomain,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := logsFilterCategories(tc.mode)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("LogsFilterAll must return nil; got %v", got)
				}
				return
			}
			if !equalCategorySet(got, tc.want) {
				t.Fatalf("logsFilterCategories(%v) = %v, want set %v", tc.mode, got, tc.want)
			}
		})
	}
}

// TestLogsFilterPartitionsKnownCategories pins the union invariant:
// every domain.KnownEventCategory must appear in exactly one of the
// three non-`all` presets. Drift would either silently hide a
// category from every chip (the user could never reach it) or
// surface it in two chips (double-render). The test fails when a
// new category lands without the chip-mapping update.
func TestLogsFilterPartitionsKnownCategories(t *testing.T) {
	seen := map[domain.EventCategory]int{}
	for _, mode := range []LogsFilterMode{
		LogsFilterToolCalls,
		LogsFilterDomain,
		LogsFilterSystem,
	} {
		for _, cat := range logsFilterCategories(mode) {
			seen[cat]++
		}
	}
	for _, cat := range domain.KnownEventCategories {
		switch seen[cat] {
		case 0:
			t.Errorf("category %q is not reachable through any non-all chip — chip mapping in logs_filter.go is out of date", cat)
		case 1:
			// expected
		default:
			t.Errorf("category %q surfaces in %d chips — chip mapping double-counts", cat, seen[cat])
		}
	}
}

// TestHandleLogsKeyCyclesFilterForward simulates an `f` keystroke
// against the live handler and asserts the Model field rolls over
// into the next preset. Mirrors the production routing: the model
// already has a non-nil Events port (the stub used elsewhere in the
// renderer tests) so the refresh path does not short-circuit before
// the mode flip lands.
func TestHandleLogsKeyCyclesFilterForward(t *testing.T) {
	model := newLogsKeyTestModel(t)
	model.handleLogsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	if model.logsFilterMode != LogsFilterToolCalls {
		t.Fatalf("f keystroke must advance LogsFilterAll → LogsFilterToolCalls, got %v", model.logsFilterMode)
	}
	model.handleLogsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	if model.logsFilterMode != LogsFilterDomain {
		t.Fatalf("second f keystroke must advance to LogsFilterDomain, got %v", model.logsFilterMode)
	}
}

// TestHandleLogsKeyCyclesFilterBackward simulates `shift+F` (which
// bubbletea surfaces as `F`) and asserts the field walks the cycle
// in reverse. The first press from LogsFilterAll must land on
// LogsFilterSystem — the wraparound rule the help text promises.
func TestHandleLogsKeyCyclesFilterBackward(t *testing.T) {
	model := newLogsKeyTestModel(t)
	model.handleLogsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'F'}})
	if model.logsFilterMode != LogsFilterSystem {
		t.Fatalf("shift+F keystroke must walk LogsFilterAll → LogsFilterSystem, got %v", model.logsFilterMode)
	}
}

// TestRenderLogsFilterChipsHighlightsActive locks the chip strip's
// visual contract: every preset label appears, the active chip
// renders bracketed so the screen reader / dim-display user has a
// non-color cue, and the cycle hint surfaces verbatim from the
// catalog so locales can translate it.
func TestRenderLogsFilterChipsHighlightsActive(t *testing.T) {
	model := newRenderLogsTestModel(t, 180)
	model.logsFilterMode = LogsFilterToolCalls

	view := ansi.Strip(model.renderLogsFilterChips())

	for _, want := range []string{"all", "tool-calls", "domain", "system", "(F cycle)"} {
		if !strings.Contains(view, want) {
			t.Errorf("chip strip missing %q\n%s", want, view)
		}
	}
	// Active chip is bracketed; inactive chips are bare.
	if !strings.Contains(view, "[ tool-calls ]") {
		t.Fatalf("active chip must render with brackets, got:\n%s", view)
	}
	if strings.Contains(view, "[ all ]") {
		t.Fatalf("inactive chip rendered with brackets — accent must be exclusive\n%s", view)
	}
}

// TestRenderLogsRefreshesChipsAcrossStates confirms the chip strip
// is included in both the populated and empty-state branches —
// otherwise an `all → tool-calls` cycle that produces zero rows
// would visually drop the chips and the user could not get back to
// `all` without remembering the key.
func TestRenderLogsRefreshesChipsAcrossStates(t *testing.T) {
	model := newRenderLogsTestModel(t, 180)
	// No events loaded → empty-state branch. The chip strip must
	// still print so the user can cycle back to `all`.
	view := ansi.Strip(model.renderLogs())
	if !strings.Contains(view, "[ all ]") {
		t.Fatalf("empty-state Logs view must keep the chip strip visible, got:\n%s", view)
	}
}

// --- helpers ---------------------------------------------------------

// newLogsKeyTestModel wraps newRenderLogsTestModel for the key-handler
// tests: the renderer helper already wires repos.Events to the empty
// stub so the cycle helper's refresh short-circuits without panicking
// on a nil receiver call.
func newLogsKeyTestModel(t *testing.T) *Model {
	t.Helper()
	model := newRenderLogsTestModel(t, 180)
	return &model
}

// equalCategorySet compares two slices as sets — order does not
// matter because EventFilter.Categories is consumed as a SQL `IN`
// clause downstream.
func equalCategorySet(a, b []domain.EventCategory) bool {
	if len(a) != len(b) {
		return false
	}
	sa := make([]string, len(a))
	sb := make([]string, len(b))
	for i, c := range a {
		sa[i] = string(c)
	}
	for i, c := range b {
		sb[i] = string(c)
	}
	sort.Strings(sa)
	sort.Strings(sb)
	return reflect.DeepEqual(sa, sb)
}
