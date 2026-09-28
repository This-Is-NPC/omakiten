package screenlayout

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const modulePrefix = "omakiten/"

// allowedImports is the complete set of in-repo packages this one may depend
// on. It is an ALLOWLIST, not a list of forbidden edges, because the thing being
// prevented is not one bad import — it is the arranger acquiring knowledge of a
// screen at all. A forbidden-prefix rule has to be extended every time a screen
// package is added; an allowlist is wrong the moment anything new appears.
var allowedImports = []string{
	"internal/keynav",
	"internal/tui/components/screenkit",
	"internal/tui/components/scrollwindow",
	"internal/tui/components/gridtable",
	"internal/tui/components/list",
	// cardtable is the join [Block.PerRow] folds its lines with. It is layout
	// math over already-painted strings — it imports lipgloss and nothing else,
	// exactly like scrollwindow above — so the edge carries no knowledge of a
	// screen, which is the thing this list exists to keep out. The alternative
	// was a second implementation of JoinHorizontal-with-a-gutter here.
	"internal/tui/components/cardtable",
}

func TestPackageIsAnImportLeaf(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("locate the package directory: %v", err)
	}
	violations, err := scanLeafViolations(dir)
	if err != nil {
		t.Fatalf("scan imports: %v", err)
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("screenlayout is not a leaf:\n  - %s\n\nallowed: %s",
			strings.Join(violations, "\n  - "), strings.Join(allowedImports, ", "))
	}
}

// TestTheLeafGateCatchesASeededViolation proves the gate above can fail.
//
// A boundary test that has never rejected anything is indistinguishable from a
// boundary test that cannot reject anything — an allowlist with a typo'd module
// prefix passes forever and silently. So the scanner is run against a seeded
// tree that imports the things it exists to keep out, and is required to name
// every one of them.
func TestTheLeafGateCatchesASeededViolation(t *testing.T) {
	seeded := map[string]string{
		"screen.go": `package screenlayout
import _ "omakiten/internal/tui/screens/board"
`,
		"host.go": `package screenlayout
import _ "omakiten/internal/tui/screenhost"
`,
		"root.go": `package screenlayout
import _ "omakiten/internal/tui"
`,
		"domain.go": `package screenlayout
import _ "omakiten/internal/domain"
`,
		// A test file is scanned too: an engine whose tests need a screen is an
		// engine with a consumer, which acceptance criterion 7 forbids.
		"pilot_test.go": `package screenlayout
import _ "omakiten/internal/tui/screens/taskdetail"
`,
		// The allowed edges must survive the same scan, or the gate is just
		// rejecting everything.
		"fine.go": `package screenlayout
import (
	_ "omakiten/internal/keynav"
	_ "omakiten/internal/tui/components/screenkit"
	_ "omakiten/internal/tui/components/scrollwindow"
	_ "omakiten/internal/tui/components/gridtable"
	_ "omakiten/internal/tui/components/list"
	_ "omakiten/internal/tui/components/cardtable"
	_ "strings"
	_ "github.com/charmbracelet/lipgloss"
)
`,
	}
	dir := t.TempDir()
	for name, src := range seeded {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}

	violations, err := scanLeafViolations(dir)
	if err != nil {
		t.Fatalf("scan the seeded tree: %v", err)
	}
	want := []string{
		"omakiten/internal/domain",
		"omakiten/internal/tui",
		"omakiten/internal/tui/screenhost",
		"omakiten/internal/tui/screens/board",
		"omakiten/internal/tui/screens/taskdetail",
	}
	for _, forbidden := range want {
		if !mentions(violations, forbidden) {
			t.Errorf("the gate did not reject %q; it reported %v", forbidden, violations)
		}
	}
	if len(violations) != len(want) {
		t.Fatalf("the gate reported %d violations, want exactly %d:\n  %s",
			len(violations), len(want), strings.Join(violations, "\n  "))
	}
}

func mentions(violations []string, importPath string) bool {
	for _, v := range violations {
		if strings.Contains(v, importPath+" ") || strings.HasSuffix(v, importPath) {
			return true
		}
	}
	return false
}

// scanLeafViolations parses every Go file in dir and reports each in-repo import
// that is not on the allowlist. Third-party and stdlib imports are ignored:
// lipgloss and x/ansi are what the arranger renders with and carry no
// architectural direction.
func scanLeafViolations(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	var violations []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.ImportsOnly)
		if err != nil {
			return nil, err
		}
		for _, imp := range file.Imports {
			raw := strings.Trim(imp.Path.Value, `"`)
			if !strings.HasPrefix(raw, modulePrefix) {
				continue
			}
			if allowed(strings.TrimPrefix(raw, modulePrefix)) {
				continue
			}
			violations = append(violations, e.Name()+" imports "+raw)
		}
	}
	return violations, nil
}

func allowed(rel string) bool {
	for _, ok := range allowedImports {
		if rel == ok || strings.HasPrefix(rel, ok+"/") {
			return true
		}
	}
	return false
}
