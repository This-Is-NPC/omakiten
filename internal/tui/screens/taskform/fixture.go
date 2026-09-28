package taskform

import (
	"fmt"

	"omakiten/internal/tui/components/field"
	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screenhost"
)

// The Task create/edit form is a stack of bordered fields with exactly one
// focus marker, a priority strip, and two message channels the screen owns —
// the validation banner above the form and the parent-lookup hint under the
// last field. None of that is reachable from an untouched form, so the four
// recordings below each park the screen on a different one: focus off its
// default field with text typed into it, the discard confirmation armed, the
// title-required banner, and an invalid parent id.
//
// DETERMINISM. Nothing on this screen is host-derived: the payload is a literal,
// the labels and theme come from the fixture, and the bubbles inputs are driven
// only by the keys replayed here. The one thing that varies with the terminal is
// the form width — AvailableWidth()-8, floored at 32 and capped at 120 — which
// is exactly what the three geometries are recorded to pin: 68 columns at the
// 80-column floor, 108 at 120, and the 120-column cap at 200.

// taskformGoldenPriorities is the configured priority strip. Five options make
// the strip wide enough that a layout change to the field row moves it.
func taskformGoldenPriorities() []PriorityOption {
	return []PriorityOption{
		{Value: "1", Label: "lowest"},
		{Value: "2", Label: "low"},
		{Value: "3", Label: "normal"},
		{Value: "4", Label: "high"},
		{Value: "5", Label: "critical"},
	}
}

// taskformGoldenDeps is the pair of host-owned dependencies: the English labels
// the host resolves from its catalog, and the bordered field chrome it builds
// from its palette. Recording against the real chrome is the point — the form's
// whole layout is border boxes, and an unstyled Theme would record bare text.
//
// The chrome comes from [field.Form] over the fixture palette, which is the
// SAME derivation the root host runs over the shipped theme. It used to be ten
// lines of hand-built lipgloss with #8AADF4 and #494D64 spelled into them: a
// second palette, in production code, that agreed with the fixture's only by
// coincidence and could not follow it when it moved.
func taskformGoldenDeps() Deps {
	return Deps{
		Theme:  field.Form(screenfixture.Styles()),
		Labels: Labels{Title: "Title", Description: "Description", Priority: "Priority", Tags: "Tags", Parent: "Parent"},
	}
}

// taskformGoldenDescription is the description the edit fixtures carry. The
// second paragraph is 110 characters, so it wraps to two rows inside the
// description box at 80 and 120 and to one at 200 — and the whole body stays
// inside the eight rows the box shows, because a bubbles textarea never scrolls
// to its caret (see TestTaskFormGoldens' note) and anything past row eight would
// simply not be in the fixture.
const taskformGoldenDescription = "Repro: 80 columns, task detail open, press e.\n" +
	"The panel border ate two columns that the viewport budget still counted for itself, so the last body row fell out.\n" +
	"\n" +
	"Expected: the last body row stays inside the panel."

// taskformGoldenTags overflows the 62-column tag input at the 80-column floor
// and fits at 200, so the fixtures record which end of it the input windows to.
const taskformGoldenTags = "tui, forms, characterization, layout-migration, goldens, three-widths"

// taskformGoldenEditPayload is an edit session over a task whose every field is
// populated past the narrow geometry's field width: the title and the tag list
// both overflow the 62-column input at the 80-column floor, so the fixtures
// record which end of each value the input windows to.
func taskformGoldenEditPayload(frame screenhost.Frame) Payload {
	return Payload{
		Mode:       Edit,
		TaskID:     2421,
		Generation: 9,
		Kicker:     fmt.Sprintf(frame.Text("tui.kicker.edit_task_fmt"), 2421),
		Values: Values{
			Title:       "Record three-width characterization goldens for the remaining uncovered screens",
			Description: taskformGoldenDescription,
			Priority:    "3",
			TagsCSV:     taskformGoldenTags,
			Parent:      "1988",
		},
		Priorities: taskformGoldenPriorities(),
	}
}

// taskformGoldenCreatePayload is the other mode: a brand-new task with nothing
// but a default priority, which is the state the title-required banner is
// reached from.
func taskformGoldenCreatePayload(frame screenhost.Frame) Payload {
	return Payload{
		Mode:       Create,
		Generation: 10,
		Kicker:     frame.Text("tui.kicker.new_task"),
		Values:     Values{Priority: "3"},
		Priorities: taskformGoldenPriorities(),
	}
}

// taskformGoldenBuild materialises the screen the way the host does: bound deps,
// an opened payload and the LifecycleEnter the host issues before the first
// paint, which is what sizes the form to the live geometry.
func taskformGoldenBuild(frame screenhost.Frame) screenhost.Screen {
	return screenfixture.Enter(New().Bind(taskformGoldenDeps()).Open(taskformGoldenEditPayload(frame), frame), frame)
}

func taskformGoldenCreateBuild(frame screenhost.Frame) screenhost.Screen {
	return screenfixture.Enter(New().Bind(taskformGoldenDeps()).Open(taskformGoldenCreatePayload(frame), frame), frame)
}

func taskformGoldenParentInvalidBuild(frame screenhost.Frame) screenhost.Screen {
	payload := taskformGoldenEditPayload(frame)
	payload.Values.Parent = "not-a-task-id"
	return screenfixture.Enter(New().Bind(taskformGoldenDeps()).Open(payload, frame), frame)
}

// taskformGoldenBind re-applies the deps between messages exactly as the host does.
func taskformGoldenBind(screen screenhost.Screen) screenhost.Screen {
	return screen.(Screen).Bind(taskformGoldenDeps())
}

// FixtureScenarios returns every recorded state for the Task create/edit form.
func FixtureScenarios() []screenfixture.Scenario {
	return []screenfixture.Scenario{
		{
			// Focus three fields along, on Tags, with text typed into it. This is
			// the fixture that pins where the single `>` focus marker sits, which
			// field's border takes the accent, and that the form renders values the
			// payload never carried.
			Name:  "edit-tags-focused",
			Build: taskformGoldenBuild,
			Bind:  taskformGoldenBind,
			Keys:  []string{"tab", "tab", "tab", ", regression-suite"},
		},
		{
			// The priority strip moved one option along and the first dirty `esc`
			// pressed. That swaps the hint line under the kicker for the discard
			// prompt, which is the only armed-confirmation state this screen owns.
			Name:  "edit-priority-discard-armed",
			Build: taskformGoldenBuild,
			Bind:  taskformGoldenBind,
			Keys:  []string{"tab", "tab", "right", "esc"},
		},
		{
			// Create mode with an empty title, saved. Validation refuses, and the
			// screen paints its own error banner between the hint line and the
			// form — a row that exists in no other state, at a width that wraps
			// differently at each geometry.
			Name:  "create-title-required",
			Build: taskformGoldenCreateBuild,
			Bind:  taskformGoldenBind,
			Keys:  []string{"ctrl+s"},
		},
		{
			// Tabbing off the Parent field blurs it, and a parent that is not a
			// positive integer fails locally rather than going to the host for a
			// lookup. The message lands on the parent hint under the last field
			// instead of the banner above the form — a different row, and the
			// screen's other error channel.
			Name:  "edit-parent-invalid",
			Build: taskformGoldenParentInvalidBuild,
			Bind:  taskformGoldenBind,
			Keys:  []string{"tab", "tab", "tab", "tab", "tab"},
		},
	}
}
