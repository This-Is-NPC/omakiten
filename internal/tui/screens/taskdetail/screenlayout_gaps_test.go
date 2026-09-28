package taskdetail

import (
	"fmt"
	"strings"
	"testing"

	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/screens/screentest"
)

func TestABorderedItemWindowIsExpressibleAsHeaderAndFooter(t *testing.T) {
	for _, geometry := range []struct {
		name          string
		width, height int
	}{{"80x24", 80, 24}, {"120x40", 120, 40}, {"200x50", 200, 50}} {
		t.Run(geometry.name, func(t *testing.T) { assertBorderedWindow(t, geometry.width, geometry.height) })
	}
}

func assertBorderedWindow(t *testing.T, width, height int) {
	t.Helper()
	frame := screentest.FrameAt(t, width, height)
	result := screenlayout.Arrange(frame.Kit(), screenlayout.NewState(), boxedSection())
	placement, ok := result.Placement(screenlayout.ID("boxed"))
	if !ok {
		t.Fatal("the boxed section was not placed")
	}
	if placement.HeaderRows != 1 || placement.FooterRows != 1 {
		t.Fatalf("the box edges were charged %d header and %d footer rows, want 1 and 1", placement.HeaderRows, placement.FooterRows)
	}
	if placement.Below == 0 {
		t.Fatal("the section is showing everything; this geometry does not exercise a closing window")
	}
	assertBoxedPaint(t, result.View, placement)
	t.Logf("bordered item-windowed section hid %d items below the fold", placement.Below)
}

func boxedSection() screenlayout.Section {
	return screenlayout.Func{
		Def: screenlayout.Spec{ID: "boxed", MinWidth: 20, MinRows: 4, Weight: 1, Scroll: screenlayout.ScrollItems},
		Body: func(canvas screenlayout.Canvas) screenlayout.Block {
			width := canvas.Width()
			edge := func(left, right string) string { return left + strings.Repeat("─", width-2) + right }
			items := make([]string, 40)
			for i := range items {
				rows := make([]string, 3)
				for j := range rows {
					body := fmt.Sprintf(" item%02d row%d", i, j)
					rows[j] = "│" + body + strings.Repeat(" ", width-2-len([]rune(body))) + "│"
				}
				items[i] = strings.Join(rows, "\n")
			}
			return screenlayout.Block{Header: []string{edge("┌", "┐")}, Items: items, Footer: []string{edge("└", "┘")}, Cursor: screenlayout.NoSelection()}
		},
	}
}

func assertBoxedPaint(t *testing.T, view string, placement screenlayout.Placement) {
	t.Helper()
	lines := strings.Split(view, "\n")
	first, last := boxedEdges(lines)
	if first < 0 || last <= first {
		t.Fatalf("no complete box was painted; got:\n%s", view)
	}
	openRows := 0
	for i := first + 1; i < last; i++ {
		line := lines[i]
		if strings.HasPrefix(line, "│") && strings.HasSuffix(line, "│") {
			continue
		}
		if strings.Contains(line, "▼") || strings.Contains(line, "▲") {
			openRows++
			continue
		}
		t.Fatalf("painted line %d is neither a boxed row nor an overflow hint: %q", i, line)
	}
	if openRows == 0 {
		t.Fatal("this geometry hid content but painted no overflow hint; the caveat below is stale")
	}
	if placement.Below == 0 {
		t.Fatal("boxed paint did not retain a hidden tail")
	}
}

func boxedEdges(lines []string) (first, last int) {
	first, last = -1, -1
	for i, line := range lines {
		if strings.HasPrefix(line, "┌") && strings.HasSuffix(line, "┐") {
			first = i
		}
		if strings.HasPrefix(line, "└") && strings.HasSuffix(line, "┘") {
			last = i
		}
	}
	return first, last
}

// TestTheArrangerHasNoSpellingForAFocusSoloStackedBody pins the gap where a
// former focus-only renderer cannot be represented by the shared arranger.
func TestTheArrangerHasNoSpellingForAFocusSoloStackedBody(t *testing.T) {
	frame := screentest.FrameAt(t, 80, 24)
	screen := New().Open(testPayload(), frame)
	result := screenlayout.Arrange(frame.Kit(), screen.grid.Layout(), screen.sections(frame)...)
	if result.Arrangement != screenlayout.Stacked {
		t.Fatalf("80x24 arranged %v, want Stacked — this gate is about the stacked body", result.Arrangement)
	}
	for _, focus := range []Focus{FocusDetails, FocusSubtasks, FocusActivity} {
		focused := screen
		focused.focus = focus
		res := screenlayout.Arrange(frame.Kit(), focused.grid.Layout(), focused.sections(frame)...)
		painted := 0
		for _, placement := range res.Placements {
			if !placement.Dropped {
				painted++
			}
		}
		if painted < 2 {
			t.Fatalf("focus %v: the arranger painted %d sections. If it has learned a focus-solo policy, this gate is STALE — delete it and re-attempt the byte-identical pure-migration commit for the 80x24 fixtures.", focus, painted)
		}
	}
	t.Log("the arranger paints every affordable section whatever holds focus; the retired screen painted one")
}
