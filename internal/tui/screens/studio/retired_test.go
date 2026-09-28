package studio

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// studioRetired is every identifier the screenlayout migration removed, with the
// reason it may never come back.
//
// The list is the executable form of acceptance criteria 1 and 2 of task #2426.
// Each of these names is a piece of the arranger written inside the screen —
// a viewport slicer, a scroll mover, a body/cursor dispatcher, or a cursor
// LOCATED rather than REPORTED — and the whole point of declaring sections is
// that a screen is never handed the inputs to write one again.
var studioRetired = map[string]string{
	"sectionBody":          "Studio has no screenlayout section; its four hosts are screengrid trees",
	"studioBodySpec":       "the unused single-body spec was folded out with the legacy section",
	"renderStudioViewport": "the screen sliced its own scroll window; screenlayout.Arrange does the windowing",
	"scrollStudioLines":    "the screen moved its own scroll offset; screenlayout.State.HandleKey owns it",
	"refreshStudioLines":   "the screen re-applied its own cursor; screenlayout.State.Resync owns the clamp",
	"refreshStudioBody":    "the body-scroll twin of refreshStudioLines, retired with it",
	"studioBodyAndCursor":  "a body string plus a line number for someone else to slice; sections carry items",
	"studioViewportRows":   "a screen-side row budget; the arranger hands the section its rows",
	"firstLineContaining":  "a cursor located by grepping the rendered text (#2417)",
	"partsLineOffset":      "a cursor line derived by counting the newlines of the blocks above it",
	"studioWrapBody":       "the source-line to wrapped-line mapping; there is one coordinate space now",
	"studioWrappedCursor":  "the translation point between the two spaces that mapping created",
	"studioScrollJump":     "an 'as far as it goes' delta the screen invented for its own offset",
	"sliceScrollRows":      "the screen's own call into the scroll window",
}

// TestStudioWritesNoneOfTheArrangerItself is the source gate behind criteria 1
// and 2. It walks the package's own non-test sources and fails on any retired
// IDENTIFIER, so the four functions the migration deletes cannot come back as a
// wrapper around the arranger — which would leave the second copy of every
// number exactly where it was.
//
// It reads source rather than exercising behaviour on purpose: a wrapper is
// behaviourally invisible, and behavioural invisibility is precisely what let
// six copies of `AvailableWidth() - chrome` survive four reviews.
//
// Identifiers, not text, because the comments in this package have to be free to
// say what was removed and why. A gate that forbade naming a retired thing in
// prose would forbid recording the defect it closed, which is the one piece of
// this migration that is worth more than the code.
func TestStudioWritesNoneOfTheArrangerItself(t *testing.T) {
	t.Parallel()

	for name, file := range studioPackageFiles(t) {
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if !ok {
				return true
			}
			if why, retired := studioRetired[ident.Name]; retired {
				t.Errorf("%s still names %q: %s", name, ident.Name, why)
			}
			return true
		})
	}
}

// studioPackageFiles is every non-test .go file in this package, parsed.
func studioPackageFiles(tb testing.TB) map[string]*ast.File {
	tb.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		tb.Fatalf("read package directory: %v", err)
	}
	fset := token.NewFileSet()
	out := map[string]*ast.File{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			tb.Fatalf("parse %s: %v", name, err)
		}
		out[name] = file
	}
	if len(out) == 0 {
		tb.Fatal("found no non-test sources; the gate would pass vacuously")
	}
	return out
}

// TestStudioRetiredGateCatchesAWrapper proves the gate above can fail. A source
// gate that never fires is indistinguishable from one that is looking in the
// wrong place, so the detection is run against a seeded violation.
func TestStudioRetiredGateCatchesAWrapper(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "seeded.go", "package studio\n\nfunc renderStudioViewport() string { return \"\" }\n", 0)
	if err != nil {
		t.Fatalf("parse seeded source: %v", err)
	}
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		if _, retired := studioRetired[ident.Name]; retired {
			found = true
		}
		return true
	})
	if !found {
		t.Fatal("the retired-identifier walk did not flag a seeded renderStudioViewport; the gate is not looking where it claims to")
	}
}
