package entitydetail

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

const entityDetailControlFixture = "漢字 😀 \x1b[31mESC\x1b]0;owned\a C0\x00 C1\u009b31m\u009d end"

func TestEntityDetailSanitizesRowsAcrossDetailStates(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		width    int
		key      string
		notFound bool
	}{
		"compact-detail": {width: 80},
		"wide-scrolled":  {width: 200, key: "G"},
		"not-found":      {width: 120, notFound: true},
	} {
		name, tc := name, tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			payload := testPayload(KindLaw)
			payload.Header = entityDetailControlFixture
			payload.Rows = []Row{{Label: entityDetailControlFixture, Value: entityDetailControlFixture}}
			payload.BodyLabel = entityDetailControlFixture
			payload.Body = "Safe 日本語\n\n" + entityDetailControlFixture
			payload.Extra = entityDetailControlFixture
			if tc.notFound {
				payload.NotFound = entityDetailControlFixture
			}
			frame := screentest.FrameAt(t, tc.width, 40)
			screen := New().Open(payload).Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
			if tc.key != "" {
				screen = screen.Update(frame, screentest.Key(tc.key)).Screen.(Screen)
			}
			assertEntityDetailRenderSafe(t, screen.View(frame))
		})
	}
}

func assertEntityDetailRenderSafe(t *testing.T, view string) {
	t.Helper()
	plain := ansi.Strip(view)
	for _, r := range plain {
		if r != '\n' && unicode.IsControl(r) {
			t.Fatalf("entity detail render retained terminal control U+%04X:\n%s", r, plain)
		}
	}
	if strings.Contains(plain, "owned") {
		t.Fatalf("entity detail render retained OSC payload:\n%s", plain)
	}
	if !strings.Contains(plain, "漢字") || !strings.Contains(plain, "😀") {
		t.Fatalf("entity detail render lost harmless Unicode:\n%s", plain)
	}
}
