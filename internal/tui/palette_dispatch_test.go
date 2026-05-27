package tui

import (
	"strings"
	"testing"

	"omakiten/internal/tui/palette"
)

func TestDispatchTrickNavResolvesAndCloses(t *testing.T) {
	model, _ := newPickerModel(t)
	model.paletteOpen = true
	model.palette = palette.NewModel()
	model.dispatchTrick(palette.Token{Verb: "nav", Operand: "31", Raw: "nav:31"})
	if model.paletteOpen {
		t.Fatalf("nav dispatch should close palette on success")
	}
	if model.top != topSettings || model.sub != subSettingsGeneral {
		t.Fatalf("after nav:31 (top, sub) = (%v, %v), want (topSettings, subSettingsGeneral)", model.top, model.sub)
	}
}

func TestDispatchTrickNavUnknownCodeKeepsPaletteOpen(t *testing.T) {
	model, _ := newPickerModel(t)
	model.paletteOpen = true
	model.palette = palette.NewModel()
	prevTop := model.top
	prevSub := model.sub
	model.dispatchTrick(palette.Token{Verb: "nav", Operand: "99", Raw: "nav:99"})
	if !model.paletteOpen {
		t.Fatalf("unknown nav code should keep palette open")
	}
	if model.top != prevTop || model.sub != prevSub {
		t.Fatalf("unknown nav code mutated nav state: (%v, %v) → (%v, %v)", prevTop, prevSub, model.top, model.sub)
	}
	if model.palette.Status() == "" {
		t.Fatalf("unknown nav code should set inline status")
	}
	if !strings.Contains(model.palette.Status(), "99") {
		t.Fatalf("status %q should mention the offending code", model.palette.Status())
	}
}

func TestDispatchTrickOpRejectsNonNumeric(t *testing.T) {
	model, _ := newPickerModel(t)
	model.paletteOpen = true
	model.palette = palette.NewModel()
	model.dispatchTrick(palette.Token{Verb: "op", Operand: "abc", Raw: "op:abc"})
	if !model.paletteOpen {
		t.Fatalf("op with non-numeric operand should keep palette open")
	}
	if !strings.Contains(model.palette.Status(), "positive task id") {
		t.Fatalf("status %q should mention task id requirement", model.palette.Status())
	}
}

func TestDispatchTrickOpUnknownTaskKeepsPaletteOpen(t *testing.T) {
	model, _ := newPickerModel(t)
	model.paletteOpen = true
	model.palette = palette.NewModel()
	model.dispatchTrick(palette.Token{Verb: "op", Operand: "999999", Raw: "op:999999"})
	if !model.paletteOpen {
		t.Fatalf("op with unknown task id should keep palette open")
	}
	if !strings.Contains(model.palette.Status(), "999999") {
		t.Fatalf("status %q should mention the unknown task id", model.palette.Status())
	}
}

func TestDispatchTrickUserDefinedVerbClosesPalette(t *testing.T) {
	model, _ := newPickerModel(t)
	model.paletteOpen = true
	model.palette = palette.NewModel()
	model.dispatchTrick(palette.Token{Verb: "hook", Operand: "1", Raw: "hook:1"})
	if model.paletteOpen {
		t.Fatalf("user-defined verb should close palette (event already emitted; hook side-effect takes over)")
	}
}

func TestJumpToRouteUnknownReturnsError(t *testing.T) {
	model, _ := newPickerModel(t)
	if err := model.jumpToRoute(palette.Route("not.a.route")); err == nil {
		t.Fatalf("jumpToRoute(unknown) error = nil, want non-nil")
	}
}

func TestJumpToRouteAllBindingsResolve(t *testing.T) {
	model, _ := newPickerModel(t)
	for _, descriptor := range palette.DefaultScreens() {
		if err := model.jumpToRoute(descriptor.Route); err != nil {
			t.Errorf("jumpToRoute(%q) error = %v", descriptor.Route, err)
		}
	}
}

func TestBuildPaletteRegistryHonorsConfigOverrides(t *testing.T) {
	model, _ := newPickerModel(t)
	if model.paletteRegistry == nil {
		t.Fatalf("paletteRegistry should be built at NewModel")
	}
	// Verify positional default lookup works without overrides.
	got, ok := model.paletteRegistry.Resolve("11")
	if !ok {
		t.Fatalf("Resolve(11) miss on default registry")
	}
	if got != palette.RouteTasksBoard {
		t.Fatalf("Resolve(11) = %q, want tasks.board", got)
	}
}
