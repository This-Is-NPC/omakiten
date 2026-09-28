package settingspicker

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

func TestLifecycleSelectionAndOutcomes(t *testing.T) {
	frame := screentest.FrameAt(t, 80, 18)
	for _, kind := range []Kind{Theme, Config, SubtaskKit} {
		t.Run(string(kind), func(t *testing.T) {
			assertSettingsPickerLifecycle(t, frame, kind)
		})
	}
}

func assertSettingsPickerLifecycle(t *testing.T, frame screenhost.Frame, kind Kind) {
	screen := New(kind).Open(testPayload(kind))
	if screen.ID() != kind.ID() || screen.Selected().Value != "active" || screen.Dirty() {
		t.Fatalf("open state = id:%q selected:%+v dirty:%v", screen.ID(), screen.Selected(), screen.Dirty())
	}
	selected := screen.Update(frame, screentest.Key("down"))
	screen = selected.Screen.(Screen)
	if screen.Selected().Value != "candidate" || !screen.Dirty() || selected.Action.Kind != screenhost.ActionNone {
		t.Fatalf("selection = selected:%+v dirty:%v action:%v", screen.Selected(), screen.Dirty(), selected.Action.Kind)
	}
	applied := screen.Update(frame, screentest.Key("enter"))
	if applied.Action.Kind != screenhost.ActionApplySettingsPicker || applied.Action.EntityKind != string(kind) || applied.Action.Value != "candidate" {
		t.Fatalf("apply action = %+v", applied.Action)
	}
	if got := screen.Update(frame, screentest.Key("esc")).Action.Kind; got != screenhost.ActionBack {
		t.Fatalf("cancel action = %v", got)
	}
	if got := screen.Update(frame, screentest.Key("r")).Action.Kind; got != screenhost.ActionReload {
		t.Fatalf("refresh action = %v", got)
	}
}

func TestRefreshResizeAndChrome(t *testing.T) {
	frame := screentest.FrameAt(t, 80, 28)
	screen := New(Config).Open(Payload{Kind: Config, Current: "item-0", Options: manyOptions(20)})
	for range 18 {
		screen = screen.Update(frame, screentest.Key("down")).Screen.(Screen)
	}
	if screen.Scroll() == 0 {
		t.Fatal("selection did not follow-scroll")
	}

	screen = screen.Refresh(Payload{Kind: Config, Current: "item-0", Options: manyOptions(3)}, nil)
	if screen.Selected().Value != "item-2" || screen.Cursor() != 2 {
		t.Fatalf("refresh did not preserve/clamp selection: cursor=%d selected=%+v", screen.Cursor(), screen.Selected())
	}
	resized := screen.Lifecycle(screentest.FrameAt(t, 140, 60), screenhost.LifecycleResize).Screen.(Screen)
	if resized.Scroll() != 0 {
		t.Fatalf("wide resize scroll = %d", resized.Scroll())
	}
	if !resized.OwnsFooter() || !resized.BlocksHostInput() || len(resized.Footer(frame)) < 5 || len(resized.Help(frame)) != 1 {
		t.Fatalf("chrome contract incomplete: footer=%v help=%v", resized.Footer(frame), resized.Help(frame))
	}
	for _, key := range []string{"enter", "esc", "r", "up", "down", "j", "k", "pgup", "pgdn", "g", "G"} {
		if !resized.OwnsKey(screentest.Key(key)) {
			t.Errorf("does not own %q", key)
		}
	}
}

func TestErrorAndEmptyStates(t *testing.T) {
	frame := screentest.FrameAt(t, 100, 28)
	if got := ansi.Strip(New(Theme).Refresh(Payload{Kind: Theme}, assertError("theme discovery failed")).View(frame)); !strings.Contains(got, "theme discovery failed") {
		t.Fatalf("error state missing:\n%s", got)
	}
	if got := ansi.Strip(New(Config).Open(Payload{Kind: Config}).View(frame)); !strings.Contains(strings.ToLower(got), "no config") {
		t.Fatalf("empty state missing:\n%s", got)
	}
}

func manyOptions(count int) []Option {
	options := make([]Option, count)
	for i := range options {
		options[i] = Option{Value: "item-" + string(rune('0'+i)), Label: "Item " + string(rune('0'+i)), Active: i == 0}
	}
	return options
}

type assertError string

func (e assertError) Error() string { return string(e) }
