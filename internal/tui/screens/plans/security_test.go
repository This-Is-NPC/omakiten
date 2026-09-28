package plans

import (
	"strings"
	"testing"
	"unicode"

	"omakiten/internal/domain"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

func TestPlanGoalMarkdownBoundarySurvivesToggle(t *testing.T) {
	frame := screentest.FrameAt(t, 100, 24)
	show := domain.PlanShow{Plan: domain.Plan{Slug: "safe", Name: "Safe plan", Status: domain.PlanStatusActive, GoalBody: "## Safe 漢字 👋\n\n**bold**\x1b[31mred\x1b]0;title\a\x00\u009b31m\n\n- bullet"}}
	screen := NewGoal().Apply(show, nil).Lifecycle(frame, screenhost.LifecycleEnter).Screen.(GoalScreen)
	for _, rendered := range []bool{true, false, true} {
		if screen.MarkdownRendered() != rendered {
			screen = screen.Update(frame, screentest.Key("M")).Screen.(GoalScreen)
		}
		view := screentest.StripANSI(screen.View(frame))
		if strings.IndexFunc(view, func(r rune) bool { return r != '\n' && unicode.IsControl(r) }) >= 0 {
			t.Fatalf("plan goal view retained terminal control: %q", view)
		}
		if !strings.Contains(view, "Safe 漢字 👋") || !strings.Contains(view, "bold") {
			t.Fatalf("plan goal view lost safe content: %q", view)
		}
	}
}
