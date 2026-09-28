package project

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/domain"
	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screens/screentest"
)

func TestProjectOverviewAndFormSanitizeMetadataAtCompactAndWideWidths(t *testing.T) {
	t.Parallel()
	const hostile = "project\x1b[31mred\x1b]0;owned\a\x00\u009b31m\u0085漢字"
	payload := fixtureBasePayload()
	payload.Project.Name, payload.Project.Slug, payload.Project.RootPath = hostile, hostile, hostile
	payload.Tags[0].Name, payload.Tags[0].Label = hostile, hostile
	payload.Dashboard.Buckets[0].Name = hostile
	for _, width := range []int{80, 200} {
		frame := screentest.FrameAt(t, width, 40)
		overview := screenfixture.Enter(New().Apply(Result{Payload: payload}), frame)
		assertProjectTerminalSafe(t, overview.View(frame))
		form := screenfixture.Enter(NewForm().Apply(payload), frame)
		assertProjectTerminalSafe(t, form.View(frame))
	}
}

func TestProjectMissingStateSanitizesSlugKicker(t *testing.T) {
	t.Parallel()
	const hostile = "project 漢字 \x1b[31mred\x1b]0;owned\a \x00\u009b31m\u009d"
	payload := fixtureBasePayload()
	payload.Project = domain.ProjectContext{Slug: hostile}
	frame := screentest.FrameAt(t, 120, 40)
	view := screenfixture.Enter(New().Apply(Result{Payload: payload}), frame).View(frame)
	assertProjectTerminalSafe(t, view)
	if !strings.Contains(ansi.Strip(view), "PROJECT 漢字 RED") {
		t.Fatalf("project state lost sanitized slug kicker:\n%s", ansi.Strip(view))
	}
}

func assertProjectTerminalSafe(t *testing.T, view string) {
	t.Helper()
	plain := ansi.Strip(view)
	for _, r := range plain {
		if r != '\n' && unicode.IsControl(r) {
			t.Fatalf("project retained terminal control U+%04X: %q", r, view)
		}
	}
	if !strings.Contains(plain, "漢字") || strings.Contains(plain, "owned") {
		t.Fatalf("project lost Unicode or retained OSC payload: %q", plain)
	}
}
