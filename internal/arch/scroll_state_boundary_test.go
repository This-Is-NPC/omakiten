package arch

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// scrollFieldAssignmentPattern matches direct mutations of any field
// named `*Scroll` on a receiver (`m.fooScroll = …` /
// `m.fooScroll += …`). The cardlist + linelist + columnframe
// component packages introduced in W11-A own the scroll field as
// unexported state; once W11-B / W11-C migrate every surface, no
// `render_*.go` file in internal/tui should write a Scroll field
// directly — every mutation routes through a typed mutator.
//
// Detection is text-based on purpose. A go/parser walk would be
// more precise but is over-engineering for an arch test the
// reviewer reads to understand WHAT the rule is — a regex makes
// the rule grep-able from the failure message.
var scrollFieldAssignmentPattern = regexp.MustCompile(`m\.[A-Za-z_][A-Za-z0-9_]*Scroll\s*[+\-]?=`)

// TestScrollStateBoundary enforces the W11 contract: surface code in
// internal/tui/render_*.go cannot write directly to a Scroll field.
// Every mutation must go through the cardlist or linelist
// component's typed mutator (MoveCursor / JumpFirst / JumpLast /
// PageUp / PageDown / WithItems / WithViewport / ScrollBy), each of
// which re-runs scrollwindow.Resync internally — the unit
// mismatch (line offset vs card index) that motivated this refactor
// becomes impossible to write.
//
// SCAFFOLDED, NOT ENFORCING. Activated by W11-D after every surface
// has been migrated. Today the test only reports the current
// violation count so the migration waves can track progress; the
// failure path is gated behind `os.Getenv("OKT_ENFORCE_SCROLL_BOUNDARY")`
// so the gate flips on with one env-var change in mise's check
// task during W11-D without touching this file.
//
// Once enforced, surfaces that introduce a new scroll-tracking
// field land here automatically; the test is the merge gate the
// reviewer no longer has to remember.
func TestScrollStateBoundary(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	renderDir := filepath.Join(root, "internal", "tui")

	var violations []string
	err = filepath.WalkDir(renderDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			// Skip the component sub-packages — they own the scroll
			// field by design and the mutations they perform are the
			// canonical implementation the boundary protects.
			if name := d.Name(); name == "components" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasPrefix(d.Name(), "render_") || !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		if strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// Iterate every match so we can build a precise report
		// (file:line) of what still needs migration.
		for _, idx := range scrollFieldAssignmentPattern.FindAllIndex(data, -1) {
			line := 1 + strings.Count(string(data[:idx[0]]), "\n")
			rel, _ := filepath.Rel(root, path)
			match := strings.TrimSpace(string(data[idx[0]:idx[1]]))
			violations = append(violations, rel+":"+itoa(line)+": "+match)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", renderDir, err)
	}

	enforce := os.Getenv("OKT_ENFORCE_SCROLL_BOUNDARY") == "1"
	if !enforce {
		// Scaffolded — log the current pre-migration count so each
		// W11-B / W11-C wave can quote the drop in its tests-passing
		// comment.
		t.Logf("scroll-state boundary scaffolded; %d direct Scroll mutations remain in render_*.go (will be enforced by W11-D)", len(violations))
		return
	}
	if len(violations) > 0 {
		t.Fatalf("scroll-state boundary violations — render_*.go cannot write a Scroll field directly; route through cardlist / linelist mutators:\n  - %s", strings.Join(violations, "\n  - "))
	}
}

// itoa is the local int-to-string helper (avoid strconv dependency
// in the test file). Cheap; n is always small (line number).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
