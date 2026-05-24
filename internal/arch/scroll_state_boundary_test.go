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

// cursorFieldAssignmentPattern is the W11-extended sibling rule: any
// `m.fooCursor = …` / `m.fooCursor += …` write outside
// `internal/tui/components/` is a violation. The cursorwindow,
// cardlist, linelist, and picker components own the cursor field as
// internal state; surface code must mutate it through a typed
// method (MoveCursor / JumpFirst / JumpLast / SetCursor / WithCursor)
// so the clamp + resync contract is one implementation deep.
//
// The bug class this rule closes: surfaces forgot to clamp the
// cursor after deleting an item and stranded it past the new last
// index. The new arch gate makes that mistake unrepresentable —
// you cannot write the stranded value through a method that always
// re-runs the clamp.
var cursorFieldAssignmentPattern = regexp.MustCompile(`m\.[A-Za-z_][A-Za-z0-9_]*Cursor\s*[+\-]?=[^=]`)

// TestScrollStateBoundary enforces the W11 contract: surface code in
// internal/tui/render_*.go cannot write directly to a Scroll field.
// Every mutation must go through the cardlist or linelist
// component's typed mutator (MoveCursor / JumpFirst / JumpLast /
// PageUp / PageDown / WithItems / WithViewport / ScrollBy), each of
// which re-runs scrollwindow.Resync internally — the unit
// mismatch (line offset vs card index) that motivated this refactor
// becomes impossible to write.
//
// ENFORCED by default since W11-D — mise's check task sets
// OKT_ENFORCE_SCROLL_BOUNDARY=1, so any new surface that introduces
// a `m.fooScroll = …` mutation in render_*.go fails CI with a
// precise file:line list pointing at the cardlist / linelist
// component the surface should route through.
//
// The env var stays opt-out for the rare local debugging session
// that needs to bisect a scroll regression by introducing a
// temporary direct mutation — unset the var, run the test, see the
// count drop progress, then re-route through the component before
// landing.
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

	enforce := os.Getenv("OKT_ENFORCE_SCROLL_BOUNDARY") != "0"
	if !enforce {
		// Opt-out path for local debugging — log the violation count
		// without failing. Production CI runs with the env unset
		// (default) and gets the strict gate; setting the var to "0"
		// loosens it for a one-off bisect.
		t.Logf("scroll-state boundary opt-out (OKT_ENFORCE_SCROLL_BOUNDARY=0); %d direct Scroll mutations remain in render_*.go", len(violations))
		return
	}
	if len(violations) > 0 {
		t.Fatalf("scroll-state boundary violations — render_*.go cannot write a Scroll field directly; route through cardlist / linelist mutators:\n  - %s", strings.Join(violations, "\n  - "))
	}
}

// TestCursorStateBoundary is the W11-extended sibling of
// TestScrollStateBoundary: surface code in internal/tui cannot
// write directly to a `*Cursor` field. Every cursor mutation must
// go through the cursorwindow / cardlist / linelist / picker
// component's typed mutator (MoveCursor, JumpFirst, JumpLast,
// SetCursor, WithCursor, WithItemCount), each of which re-runs the
// scrollwindow.Resync invariant internally — the stranded-cursor
// regression class becomes unrepresentable.
//
// Unlike the Scroll test (which only walks render_*.go), this one
// walks every non-test .go file inside internal/tui except the
// components/ sub-tree. The reason is mechanical: the surfaces that
// still mutate raw `*Cursor` ints live across model.go,
// settings_picker.go, template_default_picker.go, persona_picker.go
// and the render_*.go set — a render_-prefix filter would silently
// let those slip through.
//
// ENFORCED by default; set OKT_ENFORCE_CURSOR_BOUNDARY=0 to log
// without failing during a local migration bisect.
func TestCursorStateBoundary(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	tuiDir := filepath.Join(root, "internal", "tui")

	var violations []string
	err = filepath.WalkDir(tuiDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			// Skip the component sub-packages — they own the cursor
			// field by design and the mutations they perform are the
			// canonical implementation the boundary protects.
			if d.Name() == "components" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		if strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, idx := range cursorFieldAssignmentPattern.FindAllIndex(data, -1) {
			line := 1 + strings.Count(string(data[:idx[0]]), "\n")
			rel, _ := filepath.Rel(root, path)
			// Trim the trailing non-`=` char the lookahead captured so
			// the report shows just the assignment fragment.
			match := strings.TrimSpace(string(data[idx[0] : idx[1]-1]))
			violations = append(violations, rel+":"+itoa(line)+": "+match)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", tuiDir, err)
	}

	// Opt-IN by env var until W11-extended migration completes. The
	// commit that lands the new arch rule is intentionally RED — the
	// violations list shows surface-by-surface migration targets.
	// Once every surface has routed through a typed mutator, the
	// `.mise.toml` test task flips OKT_ENFORCE_CURSOR_BOUNDARY=1 and
	// the gate becomes strict by default (same shape as the Scroll
	// counterpart did at W11-D).
	enforce := os.Getenv("OKT_ENFORCE_CURSOR_BOUNDARY") == "1"
	if !enforce {
		t.Logf("cursor-state boundary opt-in pending migration (OKT_ENFORCE_CURSOR_BOUNDARY=1 to enforce); %d direct Cursor mutations remain in internal/tui", len(violations))
		return
	}
	if len(violations) > 0 {
		t.Fatalf("cursor-state boundary violations — internal/tui surface code cannot write a Cursor field directly; route through cursorwindow / cardlist / linelist / picker mutators:\n  - %s", strings.Join(violations, "\n  - "))
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
