package entitydetail

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

func TestPerKindActionsAndConfirmationOutcomes(t *testing.T) {
	frame := screentest.FrameAt(t, 100, 28)
	for _, tc := range []struct {
		kind Kind
		key  string
		want screenhost.ActionKind
	}{
		{KindLaw, "e", screenhost.ActionEditEntity},
		{KindSkill, "d", screenhost.ActionPrepareEntityDelete},
		{KindPersona, "p", screenhost.ActionOpenPersonaSkills},
		{KindTemplate, "a", screenhost.ActionOpenTemplateDefault},
		{KindTemplate, "d", screenhost.ActionTemplateDeleteHint},
	} {
		t.Run(string(tc.kind)+"/"+tc.key, func(t *testing.T) {
			screen := New().Open(testPayload(tc.kind))
			out := screen.Update(frame, screentest.Key(tc.key))
			if out.Action.Kind != tc.want || out.Action.EntityKind != string(tc.kind) || out.Action.Value != "alpha" {
				t.Fatalf("action = %+v, want kind=%v entity=%q value=alpha", out.Action, tc.want, tc.kind)
			}
		})
	}

	screen := New().Open(testPayload(KindLaw))
	first := screen.Update(frame, screentest.Key("d"))
	if first.Action.Kind != screenhost.ActionPrepareEntityDelete {
		t.Fatalf("prepare = %v", first.Action.Kind)
	}
	if got := first.Screen.(Screen).Update(frame, screentest.Key("d")).Action.Kind; got != screenhost.ActionDeleteEntity {
		t.Fatalf("confirm = %v", got)
	}
	if got := first.Screen.(Screen).Update(frame, screentest.Key("esc")).Action.Kind; got != screenhost.ActionCancelEntityDelete {
		t.Fatalf("cancel = %v", got)
	}
}

func TestContractChromeAndMarkdownMode(t *testing.T) {
	frame := screentest.FrameAt(t, 100, 28)
	screen := New().Open(testPayload(KindPersona))
	if screen.ID() != screenhost.EntityDetail || screen.Payload().Slug != "alpha" || !screen.OwnsFooter() || !screen.BlocksHostInput() {
		t.Fatalf("contract incomplete: id=%q payload=%+v footer=%v blocker=%v", screen.ID(), screen.Payload(), screen.OwnsFooter(), screen.BlocksHostInput())
	}
	for _, key := range []string{"esc", "e", "d", "p", "a", "r", "M", "j", "k", "pgup", "pgdn", "g", "G"} {
		if !screen.OwnsKey(screentest.Key(key)) {
			t.Errorf("does not own %q", key)
		}
	}
	if screen.OwnsKey(screentest.Key("x")) {
		t.Fatal("unexpected key ownership")
	}
	if len(screen.Footer(frame)) < 6 || len(screen.Help(frame)) != 1 {
		t.Fatalf("chrome incomplete: footer=%v help=%v", screen.Footer(frame), screen.Help(frame))
	}
	screen = screen.Update(frame, screentest.Key("M")).Screen.(Screen)
	_ = screen.View(frame)
	if screen.rendered {
		t.Fatal("markdown mode did not toggle")
	}
	if got := screen.Update(frame, struct{}{}).Screen.(Screen).Payload().Slug; got != "alpha" {
		t.Fatalf("non-key changed payload slug to %q", got)
	}
	for _, kind := range []Kind{KindLaw, KindSkill, KindTemplate} {
		candidate := New().Open(testPayload(kind))
		if len(candidate.Footer(frame)) == 0 {
			t.Errorf("%s footer empty", kind)
		}
		for _, key := range []string{"p", "a"} {
			_ = candidate.Update(frame, screentest.Key(key))
		}
	}
}

func TestLifecycleRefreshResizeAndBack(t *testing.T) {
	frame := screentest.FrameAt(t, 58, 12)
	screen := New().Open(testPayload(KindLaw))
	for range 8 {
		screen = screen.Update(frame, screentest.Key("down")).Screen.(Screen)
	}
	if screen.Scroll() == 0 {
		t.Fatal("detail did not scroll")
	}
	resized := screen.Lifecycle(screentest.FrameAt(t, 140, 60), screenhost.LifecycleResize).Screen.(Screen)
	if resized.Scroll() != 0 {
		t.Fatalf("wide resize scroll = %d", resized.Scroll())
	}
	if got := resized.Update(frame, screentest.Key("r")).Action.Kind; got != screenhost.ActionReload {
		t.Fatalf("refresh action = %v", got)
	}
	if got := resized.Update(frame, screentest.Key("esc")).Action.Kind; got != screenhost.ActionBack {
		t.Fatalf("back action = %v", got)
	}
	if entered := resized.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen); entered.Scroll() != 0 {
		t.Fatalf("enter scroll = %d", entered.Scroll())
	}
}

func TestLoadingErrorAndNotFound(t *testing.T) {
	frame := screentest.FrameAt(t, 100, 28)
	for name, screen := range map[string]Screen{
		"loading":   New().Loading(),
		"error":     New().Apply(Payload{}, assertError("detail failed")),
		"not-found": New().Open(Payload{NotFound: "Template not found"}),
	} {
		got := ansi.Strip(screen.View(frame))
		valid := strings.Contains(strings.ToLower(got), strings.ReplaceAll(name, "-", " "))
		valid = valid || name == "error" && strings.Contains(got, "detail failed")
		valid = valid || name == "not-found" && strings.Contains(got, "Template not found")
		if !valid {
			t.Errorf("%s state missing:\n%s", name, got)
		}
	}
}

type assertError string

func (e assertError) Error() string { return string(e) }
