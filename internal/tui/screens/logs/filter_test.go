package logs

import (
	"reflect"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"

	"omakiten/internal/domain"
)

// TestLogsFilterCycleForwardOrder pins the umbrella's AC #2 chip
// rotation: `all → tool-calls → domain → system → all`. The test
// walks the cycle helper once per step and asserts the sequence
// returned matches the documented order — failure means a chip drift
// has landed in filter.go and the help-row description in
// en.yaml + docs/screens.md is out of sync with the runtime.
func TestLogsFilterCycleForwardOrder(t *testing.T) {
	t.Parallel()
	want := []domain.LogsFilterMode{
		FilterAll,
		FilterToolCalls,
		FilterDomain,
		FilterSystem,
		FilterAll, // rollover
	}
	got := []domain.LogsFilterMode{FilterAll}
	mode := FilterAll
	for i := 0; i < len(want)-1; i++ {
		mode = domain.CycleLogsFilter(mode, 1)
		got = append(got, mode)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("forward cycle mismatch (-want +got):\n%s", diff)
	}
}

// TestLogsFilterCycleBackwardOrder pins the reverse rotation triggered
// by shift+F. Walking backward from `all` must land on `system` first
// (umbrella AC #2 covers forward; reverse is the inverse and the
// scope description in the task also calls it out explicitly).
func TestLogsFilterCycleBackwardOrder(t *testing.T) {
	t.Parallel()
	want := []domain.LogsFilterMode{
		FilterAll,
		FilterSystem,
		FilterDomain,
		FilterToolCalls,
		FilterAll, // rollover
	}
	got := []domain.LogsFilterMode{FilterAll}
	mode := FilterAll
	for i := 0; i < len(want)-1; i++ {
		mode = domain.CycleLogsFilter(mode, -1)
		got = append(got, mode)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("backward cycle mismatch (-want +got):\n%s", diff)
	}
}

// TestLogsFilterCategoriesMapping pins the mode → repository-filter
// projection. The slice equality is order-insensitive on purpose:
// EventFilter.Categories is consumed as a set (SQL IN clause) so the
// invariant is membership, not order. Failure here means the panel
// rows the user sees will no longer match the chip label.
func TestLogsFilterCategoriesMapping(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		mode domain.LogsFilterMode
		want []domain.EventCategory
	}{
		{
			name: "all → nil (no filter)",
			mode: FilterAll,
			want: nil,
		},
		{
			name: "tool-calls → tool_call + hook",
			mode: FilterToolCalls,
			want: []domain.EventCategory{
				domain.EventCategoryToolCall,
				domain.EventCategoryHook,
			},
		},
		{
			name: "domain → task / comment / plan / trick / tag-dep",
			mode: FilterDomain,
			want: []domain.EventCategory{
				domain.EventCategoryTask,
				domain.EventCategoryComment,
				domain.EventCategoryPlan,
				domain.EventCategoryTrick,
				domain.EventCategoryTagDep,
			},
		},
		{
			name: "system → audit / guard / domain / update / tui",
			mode: FilterSystem,
			want: []domain.EventCategory{
				domain.EventCategoryAudit,
				domain.EventCategoryGuard,
				domain.EventCategoryDomain,
				domain.EventCategoryUpdate,
				domain.EventCategoryTUI,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := domain.LogsFilterCategories(tc.mode)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("FilterAll must return nil; got %v", got)
				}
				return
			}
			if !equalCategorySet(got, tc.want) {
				t.Fatalf("FilterCategories(%v) = %v, want set %v", tc.mode, got, tc.want)
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
	t.Parallel()
	seen := map[domain.EventCategory]int{}
	for _, mode := range []domain.LogsFilterMode{
		FilterToolCalls,
		FilterDomain,
		FilterSystem,
	} {
		for _, cat := range domain.LogsFilterCategories(mode) {
			seen[cat]++
		}
	}
	for _, cat := range domain.KnownEventCategories {
		switch seen[cat] {
		case 0:
			t.Errorf("category %q is not reachable through any non-all chip — chip mapping in filter.go is out of date", cat)
		case 1:
			// expected
		default:
			t.Errorf("category %q surfaces in %d chips — chip mapping double-counts", cat, seen[cat])
		}
	}
}

// TestLogsFilterPartitionMapMatchesEnumeration locks the domain-owned
// FilterMode → category partition. Every chip must resolve deterministically.
func TestLogsFilterPartitionMapMatchesEnumeration(t *testing.T) {
	t.Parallel()
	for _, f := range []domain.LogsFilterMode{
		FilterAll,
		FilterToolCalls,
		FilterDomain,
		FilterSystem,
	} {
		if f != FilterAll && len(domain.LogsFilterCategories(f)) == 0 {
			t.Errorf("FilterMode %v has no domain category partition", f)
		}
	}
}

// --- helpers ---------------------------------------------------------

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
