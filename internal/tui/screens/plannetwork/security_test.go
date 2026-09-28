package plannetwork

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/domain"
	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screens/screentest"
)

const planNetworkControlFixture = "漢字 😀 \x1b[31mESC\x1b]0;owned\a C0\x00 C1\u009b31m\u009d end"

func TestPlanNetworkSanitizesNodeLabelsAcrossStates(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		width  int
		cursor int
	}{
		"compact":       {width: 80, cursor: 2},
		"wide-selected": {width: 200, cursor: 12},
	} {
		name, tc := name, tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			frame := screentest.FrameAt(t, tc.width, 40)
			screen := screenfixture.Enter(New().Open(planNetworkSecurityPayload()), frame).(Screen)
			screen = screen.SelectCursor(tc.cursor)
			assertPlanNetworkRenderSafe(t, screen.View(frame))
		})
	}
}

func TestPlanNetworkEmptyStateSanitizesHostileSlug(t *testing.T) {
	t.Parallel()
	slug := string([]byte("empty-\x9downed\x9c 漢字 😀"))
	screen := New().Open(Payload{Show: domain.PlanShow{Plan: domain.Plan{Slug: slug}}})
	view := screen.View(screentest.FrameAt(t, 100, 24))
	plain := ansi.Strip(view)
	if !utf8.ValidString(plain) {
		t.Fatalf("empty-state render retained invalid UTF-8: %q", plain)
	}
	assertPlanNetworkRenderSafe(t, view)
	if !strings.Contains(plain, "empty-") {
		t.Fatalf("empty-state render lost the safe slug prefix: %q", plain)
	}
}

func planNetworkSecurityPayload() Payload {
	payload := planNetworkGoldenPayload()
	payload.Show.Plan.Slug = planNetworkControlFixture
	for i := range payload.Show.Waves {
		payload.Show.Waves[i].Wave.Name = planNetworkControlFixture
		for j := range payload.Show.Waves[i].Tasks {
			payload.Show.Waves[i].Tasks[j].Title = planNetworkControlFixture
			payload.Show.Waves[i].Tasks[j].BucketKey = planNetworkControlFixture
			payload.Show.Waves[i].Tasks[j].AssignedTo = planNetworkControlFixture
		}
	}
	return payload
}

func assertPlanNetworkRenderSafe(t *testing.T, view string) {
	t.Helper()
	plain := ansi.Strip(view)
	for _, r := range plain {
		if r != '\n' && unicode.IsControl(r) {
			t.Fatalf("plan network render retained terminal control U+%04X:\n%s", r, plain)
		}
	}
	if strings.Contains(plain, "owned") {
		t.Fatalf("plan network render retained OSC payload:\n%s", plain)
	}
	if !strings.Contains(plain, "漢字") || !strings.Contains(plain, "😀") {
		t.Fatalf("plan network render lost harmless Unicode:\n%s", plain)
	}
}
