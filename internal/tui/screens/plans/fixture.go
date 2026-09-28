package plans

import (
	"fmt"
	"strings"

	"omakiten/internal/domain"
	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screenhost"
)

// The package owns two screens, and the baseline records both because a layout
// migration touches them for different reasons.
//
// The LIST is a fixed-column panel whose name column is whatever is left after
// a 58-cell block — 14 cells at the 80-column floor against 54 at 120 and 134
// at 200 — so the plan name plus its "‹active wave›" suffix is cut to almost
// nothing at the floor and printed whole at the widest terminal. Forty rollups
// against 7, 23 and 33 cursor rows means every recorded list is a window.
//
// The GOAL reader is a header block over a scrolling markdown body, which is a
// different failure mode: the value column is width-dependent, the body is
// routed through a host-supplied renderer, and `M` swaps the rendered body for
// its raw source. Both of those states are recorded, one of them parked at the
// bottom of a document that overflows even the tallest recorded terminal.

// plansGoldenRollups is the fixture list: 40 plans whose slugs and names both
// run past the columns that hold them, with the three plan statuses and a
// mixture of complete, partial, empty and zero-total progress so the percentage
// column prints every shape it can.
func plansGoldenRollups() []domain.PlanRollup {
	names := []string{
		"Screen extraction cohort four and the characterization baseline that guards it",
		"Guard catalog",
		"Workflow v2 orchestrator delegation and the command slots it fills",
		"Locale parity",
		"Board and table row budget consolidation across every card surface",
		"Release hardening",
	}
	waves := []string{"Build", "Review and handoff", "", "Record baseline", "Ship"}
	statuses := []domain.PlanStatus{domain.PlanStatusActive, domain.PlanStatusDone, domain.PlanStatusAbandoned}
	rollups := make([]domain.PlanRollup, 0, 40)
	for i := 0; i < 40; i++ {
		total := (i * 3) % 17
		done := 0
		if total > 0 {
			done = (i * 5) % (total + 1)
		}
		rollups = append(rollups, domain.PlanRollup{
			Plan: domain.Plan{
				ID:     int64(i + 1),
				Slug:   fmt.Sprintf("plan-%02d-%s", i+1, []string{"screen-extraction", "guards", "workflow", "i18n"}[i%4]),
				Name:   names[i%len(names)],
				Status: statuses[i%len(statuses)],
			},
			DoneCount:      done,
			TotalCount:     total,
			ActiveWaveName: waves[i%len(waves)],
		})
	}
	return rollups
}

// plansGoldenGoalBody is the goal document under test: a paragraph that wraps at
// every recorded width, a bulleted block, and enough numbered lines to overflow
// the 40-row body the widest recorded terminal gives the reader.
func plansGoldenGoalBody() string {
	var b strings.Builder
	b.WriteString("The plan goal has to survive a paragraph long enough that it wraps at every recorded width, because the wrap column is the first thing a layout migration moves.\n\n")
	b.WriteString("- a bullet\n- a second bullet whose text runs past the 80-column floor and therefore wraps there but not at 200\n- a third\n\n")
	for i := 1; i <= 60; i++ {
		fmt.Fprintf(&b, "%02d. goal line %02d\n", i, i)
	}
	return b.String()
}

// plansGoldenShow is the plan the reader is opened on. The name is long enough
// to wrap inside the value column at 80 and not at 120, so the header block's
// wrap is recorded alongside the body's.
func plansGoldenShow() domain.PlanShow {
	return domain.PlanShow{
		Plan: domain.Plan{
			ID:       11,
			Slug:     "plan-11-screen-extraction",
			Name:     "Screen extraction cohort four and the characterization baseline that guards it",
			Status:   domain.PlanStatusActive,
			GoalBody: plansGoldenGoalBody(),
		},
		DoneCount:  4,
		TotalCount: 11,
	}
}

func plansGoldenListBuild(frame screenhost.Frame) screenhost.Screen {
	return screenfixture.Enter(New().Apply(plansGoldenRollups(), nil), frame)
}

func plansGoldenGoalBuild(frame screenhost.Frame) screenhost.Screen {
	return screenfixture.Enter(NewGoal().Apply(plansGoldenShow(), nil), frame)
}

func listFixtureScenarios() []screenfixture.Scenario {
	return []screenfixture.Scenario{
		{
			// Mid-list: two page steps and a single one, which land on a
			// different row at each geometry because a page step is half the
			// panel's row budget. Rows hidden above AND below the slice, so both
			// scroll hints are recorded next to the truncated names.
			Name:  "list-middle",
			Build: plansGoldenListBuild,
			Keys:  []string{"pgdown", "pgdown", "j"},
		},
		{
			// The bottom of the list, the one scroll position a clamp regression
			// moves without moving anything else: 40 plans against a 33-row
			// cursor window at the widest geometry.
			Name:  "list-bottom",
			Build: plansGoldenListBuild,
			Keys:  []string{"G"},
		},
	}
}

func goalFixtureScenarios() []screenfixture.Scenario {
	return []screenfixture.Scenario{
		{
			// The reader in its entry mode — rendered markdown — scrolled a page
			// and four lines in. A page step is a function of the terminal
			// height, so the extra single steps keep the three geometries landing
			// on different content, which is the point of recording three.
			Name:  "goal-rendered",
			Build: plansGoldenGoalBuild,
			Keys:  []string{"pgdown", "j", "j", "j", "j"},
		},
		{
			// The same document with `M` pressed: raw source, which bypasses the
			// renderer's wrapping so the reader's own wrap is what is recorded.
			// `G` parks it at the bottom.
			Name:  "goal-raw-bottom",
			Build: plansGoldenGoalBuild,
			Keys:  []string{"M", "G"},
		},
	}
}

// FixtureScenariosFor returns the scenarios for one screen ID, or nil when the
// ID is not owned by this package.
func FixtureScenariosFor(id screenhost.ID) []screenfixture.Scenario {
	switch id {
	case screenhost.TasksPlans:
		return listFixtureScenarios()
	case screenhost.PlanGoal:
		return goalFixtureScenarios()
	default:
		return nil
	}
}
