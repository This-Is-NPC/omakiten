package commentdetail

import (
	"errors"
	"fmt"
	"strings"

	"omakiten/internal/domain"
	"omakiten/internal/tui/components/field"
	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screenhost"
)

// The Comment detail screen is two screens behind one route: a label-row header
// over a scrolling body in read mode, and a bordered textarea over a hint line
// in edit mode. The four recordings below cover both, plus the two states the
// screen owns that are neither — the raw-markdown toggle with the delete
// confirmation armed, and an edit session carrying a failed save.
//
// DETERMINISM. The one value this screen prints that a clock could reach is the
// comment's timestamp, and domain.Comment carries CreatedAt as an already
// formatted string rather than a time — so the fixture pins it to a literal in
// commentGoldenComment and nothing on the render path consults a clock. Body
// markdown paints through components/markdown with TrueColor forced, so the
// same body at the same width yields the same ANSI under TERM=dumb and under a
// truecolor terminal. The tag list is a slice, not a map.

// commentGoldenBody is the comment under test: a paragraph that wraps well past
// 80 columns, a bulleted block, and enough numbered lines to overflow the 40-row
// body the tallest recorded terminal gets.
func commentGoldenBody() string {
	var b strings.Builder
	b.WriteString("The comment reader has to survive a paragraph long enough that it wraps at every recorded width, because the wrap column is the first thing a layout migration moves.\n\n")
	b.WriteString("- a bullet\n- a second bullet whose text runs past the 80-column floor and therefore wraps there but not at 200\n- a third\n\n")
	for i := 1; i <= 60; i++ {
		fmt.Fprintf(&b, "%02d. body line %02d\n", i, i)
	}
	return b.String()
}

// commentGoldenComment is the comment the screen is opened on. Every field the
// header block can print is populated — task scope, author, timestamp and a tag
// list long enough to wrap inside the value column at 80 and not at 200 — so
// the fixtures pin the whole header, not the two rows a bare comment fills.
//
// CreatedAt is a pinned literal. It is the only field on this screen that would
// otherwise be a clock reading, and a fixture that re-renders as "2 hours ago"
// tomorrow records nothing.
func commentGoldenComment() domain.Comment {
	return domain.Comment{
		ID:         4821,
		ProjectID:  1,
		TaskID:     2421,
		Scope:      domain.CommentScopeTask,
		AuthorType: "agent",
		CreatedAt:  "2026-02-14 09:31:07",
		UpdatedAt:  "2026-02-14 09:31:07",
		Body:       commentGoldenBody(),
		Tags: []domain.Tag{
			{ID: 1, Name: "tests-passing", Label: "tests-passing"},
			{ID: 2, Name: "characterization-baseline", Label: "characterization-baseline"},
			{ID: 3, Name: "layout-migration", Label: "layout-migration"},
		},
	}
}

func commentGoldenPayload() Payload {
	return Payload{Comment: commentGoldenComment(), Editable: true, Deletable: true}
}

// commentGoldenEditBody is the body the two EDIT recordings open on, and it is
// short on purpose: a bordered textarea only ever shows its first `EditHeight`
// display rows, so a body taller than the box would put the caret — and every
// character the recording types — outside the recorded bytes. See
// TestCommentDetailGoldens' note on the caret-scroll defect: this fixture is
// sized so the fixtures record the editor's chrome and the user's text rather
// than that defect.
//
// The second line is 127 characters, which wraps to two rows inside the edit
// box at the 80-column floor and at 120, and to one at 200 — so the three
// geometries still record three different boxes.
const commentGoldenEditBody = "Reproduced on the 80-column floor.\n" +
	"The panel border ate two columns that the viewport budget still counted, so the last row of the body was drawn under the footer.\n" +
	"Patch attached."

// commentGoldenEditPayload is the same comment metadata over a body that fits
// the edit box at every recorded geometry.
func commentGoldenEditPayload() Payload {
	comment := commentGoldenComment()
	comment.Body = commentGoldenEditBody
	return Payload{Comment: comment, Editable: true, Deletable: true}
}

// commentGoldenDeps is the edit-form chrome the host builds from its palette,
// so the edit fixtures record the real box the textarea sits in rather than an
// unstyled rectangle. Body markdown paints through components/markdown with
// TrueColor forced — there is no RenderBody stand-in.
//
// It comes from [field.Multiline] over the fixture palette — the SAME derivation
// the root host runs over the shipped theme — rather than from three hand-built
// styles with #494D64 and #8AADF4 spelled into them. A second palette in
// production code agreed with the fixture's only by coincidence.
func commentGoldenDeps() Deps {
	return Deps{EditTheme: field.Multiline(screenfixture.Styles())}
}

// commentGoldenBuild materialises the read-mode screen the way the host does —
// bound deps, an opened payload, and the LifecycleEnter that resets the body
// viewport.
func commentGoldenBuild(frame screenhost.Frame) screenhost.Screen {
	return screenfixture.Enter(New().Bind(commentGoldenDeps()).Open(commentGoldenPayload()), frame)
}

// commentGoldenEditBuild materialises the edit payload with the same enter path
// the edit fixtures need so the textarea is sized to the live geometry.
func commentGoldenEditBuild(frame screenhost.Frame) screenhost.Screen {
	return screenfixture.Enter(New().Bind(commentGoldenDeps()).Open(commentGoldenEditPayload()), frame)
}

// commentGoldenDriveKey drives one key through the real Update path with the
// deps re-bound first, exactly as the host re-binds between messages.
func commentGoldenDriveKey(screen Screen, frame screenhost.Frame, key string) Screen {
	out := screen.Bind(commentGoldenDeps()).Update(frame, screenfixture.Key(key))
	next, ok := out.Screen.(Screen)
	if !ok {
		panic("commentdetail: key " + key + " returned no comment screen")
	}
	return next
}

// commentGoldenSaveFailedBuild replays the host round trip that lands on a
// failed save: edit mode entered, text typed, save attempted, and the repository
// error folded back through Apply. Keys alone cannot reach this state.
func commentGoldenSaveFailedBuild(frame screenhost.Frame) screenhost.Screen {
	screen := commentGoldenEditBuild(frame).(Screen)
	screen = commentGoldenDriveKey(screen, frame, "e")
	screen = commentGoldenDriveKey(screen, frame, " This edit never landed.")
	screen = commentGoldenDriveKey(screen, frame, "ctrl+s")
	return screen.Apply(Result{CommentID: commentGoldenComment().ID, Err: errors.New("save comment 4821: database is locked")})
}

func commentGoldenBind(screen screenhost.Screen) screenhost.Screen {
	return screen.(Screen).Bind(commentGoldenDeps())
}

// FixtureScenarios returns every recorded state for the Comment detail screen.
func FixtureScenarios() []screenfixture.Scenario {
	return []screenfixture.Scenario{
		{
			// Read mode over rendered markdown, four lines down. Four rather
			// than a page on purpose: it is far enough that the body is
			// demonstrably windowed, and near enough that the header block —
			// the task row, the author, the pinned timestamp and the wrapping
			// tag list — is still on screen. The deep-scroll case is
			// raw-bottom-delete-armed's job.
			Name:  "read-scrolled",
			Build: commentGoldenBuild,
			Bind:  commentGoldenBind,
			Keys:  []string{"j", "j", "j", "j"},
		},
		{
			// `M` swaps the body to raw source, so the renderer's wrapping is
			// out of the way and the grid's own wrap is what is recorded. `G`
			// parks it at the bottom — the one scroll position a clamp
			// regression moves without moving anything else — and `d` arms the
			// delete confirmation last, because every other key disarms it.
			Name:  "raw-bottom-delete-armed",
			Build: commentGoldenBuild,
			Bind:  commentGoldenBind,
			Keys:  []string{"M", "G", "d"},
		},
		{
			// The editor open over a body the user has already added to. This
			// is the dirty fixture: the textarea holds text the payload does
			// not, so a migration that resized or re-chromed the edit box moves
			// these bytes.
			Name:  "edit-dirty",
			Build: commentGoldenEditBuild,
			Bind:  commentGoldenBind,
			Keys:  []string{"e", " Re-measured against the 120-column default too."},
		},
		{
			// A save that came back failed, which is the only way the error
			// banner above the edit box is reachable: ctrl+s hands the host an
			// action, and the host folds the repository's error back in through
			// Apply. Keys alone cannot get here, so Build replays the whole
			// round trip — freshly, on every call, like every other Build.
			Name:  "edit-save-failed",
			Build: commentGoldenSaveFailedBuild,
			Bind:  commentGoldenBind,
		},
	}
}
