package description

import (
	"fmt"
	"strings"

	"omakiten/internal/domain"
	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screenhost"
)

// The Task Description reader is a header block over a scrolling body, so the
// two states worth protecting are the two the body can be in: rendered
// markdown scrolled into the middle of a long document, and the raw source the
// `M` toggle swaps to. Both bodies are long enough to overflow the tallest
// recorded terminal and wide enough to wrap at the 80-column floor, so the
// fixtures pin the wrap, the scroll window and the overflow hint rather than a
// single screenful that happens to fit.
//
// The overflow hint is the arranger's split ("▲ N above" / "▼ N below"), not
// the combined Viewport.Fit footer this screen used to paint.

// goldenBody is the description under test: a paragraph that wraps well past
// 80 columns, a bulleted block, and enough numbered lines to overflow 50 rows.
// Body markdown paints through components/markdown with TrueColor forced, so
// the recorded bytes are the real glamour structure (headings, bullets, wrap
// columns) rather than a fake hard wrap.
func goldenBody() string {
	var b strings.Builder
	b.WriteString("The reader has to survive a paragraph long enough that it wraps at every recorded width, because the wrap column is the first thing a layout migration moves.\n\n")
	b.WriteString("- a bullet\n- a second bullet whose text runs past the 80-column floor and therefore wraps there but not at 200\n- a third\n\n")
	for i := 1; i <= 60; i++ {
		fmt.Fprintf(&b, "%02d. body line %02d\n", i, i)
	}
	return b.String()
}

// goldenTask is the task the reader is opened on. The title is long enough to
// wrap inside the value column at 80 and not at 120, so the header block's
// wrap is recorded too.
func goldenTask() domain.Task {
	return domain.Task{
		ID:          2421,
		Title:       "Record 3-width characterization goldens for the remaining uncovered screens",
		BucketKey:   "dev",
		Description: goldenBody(),
	}
}

func goldenBuild(frame screenhost.Frame) screenhost.Screen {
	return screenfixture.Enter(New().Open(goldenTask()), frame)
}

// FixtureScenarios returns every recorded state for the Task Description reader.
func FixtureScenarios() []screenfixture.Scenario {
	return []screenfixture.Scenario{
		{
			// Rendered markdown, scrolled a page and six lines in. The page is
			// the arranger's pageItems (data rows, not the Viewport half-page
			// this screen used to own), so the extra single steps keep the
			// three geometries landing on different content.
			Name:  "rendered",
			Build: goldenBuild,
			Keys:  []string{"pgdown", "j", "j", "j", "j", "j", "j"},
		},
		{
			// The same document with `M` pressed: raw source, and the body
			// bypasses the renderer's wrapping so the screen's own wrap is what
			// is recorded. `G` parks it at the bottom, the one scroll position
			// a clamp regression moves without moving anything else.
			Name:  "raw-bottom",
			Build: goldenBuild,
			Keys:  []string{"M", "G"},
		},
	}
}
