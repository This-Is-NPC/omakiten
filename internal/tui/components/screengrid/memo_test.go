package screengrid

import (
	"testing"

	"omakiten/internal/tui/components/screenlayout"
)

func TestHandleKeyReusesEquivalentGridFrame(t *testing.T) {
	kit := testKit()
	b := box(80, 20)
	calls := 0
	items := []string{"one", "two", "three", "four"}
	body := func(canvas screenlayout.Canvas) screenlayout.Block {
		calls++
		return screenlayout.Block{Items: items, Cursor: screenlayout.At(canvas.Cursor())}
	}
	root := Cell(screenlayout.Spec{
		ID:          "body",
		MinRows:     4,
		Scroll:      screenlayout.ScrollItems,
		SelectFirst: true,
	}, body)

	state := NewState().Resync(kit, b, root).WithFocus("body")
	calls = 0
	next, handled := state.HandleKey(kit, b, "j", root)
	if !handled {
		t.Fatal("j was not handled")
	}
	if calls != 0 {
		t.Fatalf("motion key composed the body %d time(s); want the equivalent frame", calls)
	}

	calls = 0
	Render(kit, next, b, root)
	if calls == 0 {
		t.Fatal("render reused the memo and did not repaint the body")
	}
}

func TestHandleKeyGridFrameMissesWhenInputsChange(t *testing.T) {
	kit := testKit()
	b := box(80, 20)
	calls := 0
	items := []string{"one", "two", "three", "four"}
	body := func(canvas screenlayout.Canvas) screenlayout.Block {
		calls++
		return screenlayout.Block{Items: items, Cursor: screenlayout.At(canvas.Cursor())}
	}
	spec := screenlayout.Spec{
		ID:          "body",
		MinRows:     4,
		Scroll:      screenlayout.ScrollItems,
		SelectFirst: true,
	}
	root := Cell(spec, body)

	cases := map[string]struct {
		state State
		box   screenlayout.Box
		root  Node
	}{
		"spec": {
			state: NewState().Resync(kit, b, root).WithFocus("body"),
			box:   b,
			root:  Cell(specWithMinRows(spec, 5), body),
		},
		"box": {
			state: NewState().Resync(kit, b, root).WithFocus("body"),
			box:   box(64, 20),
			root:  root,
		},
		"fullscreen": {
			state: NewState().Resync(kit, b, root).WithFocus("body").withFullscreen(true),
			box:   b,
			root:  root,
		},
		"path": {
			state: NewState().Resync(kit, b, root).WithFocus("body").enter("entered", "body"),
			box:   b,
			root:  root,
		},
		"focus": {
			state: NewState().Resync(kit, b, root).WithFocus("other"),
			box:   b,
			root:  root,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			calls = 0
			_, handled := tc.state.HandleKey(kit, tc.box, "j", tc.root)
			if !handled {
				t.Fatal("j was not handled")
			}
			if calls == 0 {
				t.Fatal("changed frame inputs reused the old frame without composing the body")
			}
		})
	}
}

func specWithMinRows(spec screenlayout.Spec, rows int) screenlayout.Spec {
	spec.MinRows = rows
	return spec
}
