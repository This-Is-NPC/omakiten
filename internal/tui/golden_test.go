package tui

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// updateGolden gates `os.WriteFile` on the testdata snapshots. CI never sets
// it, so a drift fails the test; locally `go test ./internal/tui/ -update`
// rewrites the file. Documented in CONTRIBUTING.md as the standard pattern.
var updateGolden = flag.Bool("update", false, "rewrite golden testdata files")

// TestRenderSystemEventCard_Golden is the reference for the golden-file
// pattern documented in CONTRIBUTING.md. Captures the rendered output of
// renderSystemEventCard with ANSI escapes stripped so the snapshot is
// stable across terminal-color profiles. Inputs are pinned (fixed width,
// fixed event payload) so any drift in the renderer surfaces as a diff.
func TestRenderSystemEventCard_Golden(t *testing.T) {
	m := Model{
		styles: newStyles(config.Theme{}),
		width:  100,
	}
	ev := domain.Event{
		ID:        42,
		EventType: domain.EventTypeTaskMoved,
		Payload:   `{"from":"backlog","to":"dev"}`,
		CreatedAt: "2026-05-09 12:00:00",
	}

	got := stripANSI(m.renderSystemEventCard(ev, false))
	path := filepath.Join("testdata", "system_event_card.golden")

	if *updateGolden {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if got != string(want) {
		t.Fatalf("golden mismatch — re-run with -update to refresh.\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}
