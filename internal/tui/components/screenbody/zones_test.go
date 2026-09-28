package screenbody_test

import (
	"testing"

	"omakiten/internal/tui/components/screenbody"
	"omakiten/internal/tui/components/screenlayout"
)

func TestSpecDefaultGeometry(t *testing.T) {
	got := screenbody.Spec("some-id")
	want := screenlayout.Spec{ID: "some-id", MinRows: 1, Weight: 1, Scroll: screenlayout.ScrollItems}
	if got != want {
		t.Errorf("Spec() = %+v, want %+v", got, want)
	}
}

func TestNewRejectsAnExplicitZeroSpec(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("New accepted a section without a focus identity")
		}
	}()
	_ = screenbody.New(screenlayout.Spec{}, nil)
}
